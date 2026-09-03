package post

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// SinkResult is one sink's outcome for one post.
//
// Reason and Err are separate on purpose (FR-017). Reason is the only field a
// front door may show: a short classified phrase such as "request timed out" or
// "chat not found", drawn from a fixed set. Err carries the underlying error
// for the diagnostic log and is never rendered to the user, because an error
// chain can carry a URL, a header, or a credential that a display string must
// not (FR-029, FR-043, FR-069).
//
// Both fields are populated by the orchestrator rather than by the sink, so the
// two sinks cannot acquire different failure vocabularies (constitution
// principle II). The fixed-set guarantee for Reason is enforced by the error
// classifier and its test, not by this type.
//
// Keeping Err out of a display string is not something a field comment can
// enforce, because the default Go renders reach it without anyone asking. The
// bot token sits in the Telegram request path, so a transport failure yields a
// *url.Error whose exported URL field carries the token; a whole result printed
// from any front door, log line, or debug dump would then print the secret.
//
// Five separate mechanisms reach a value's fields that way, each with its own
// interface and its own precedence: fmt.Formatter (consulted for every verb),
// fmt.Stringer (%v, %s, %q, %x, %X), fmt.GoStringer (%#v), json.Marshaler
// (encoding/json), and slog.LogValuer (every slog attribute). Format, String,
// GoString, MarshalJSON and LogValue below implement all five, and every one of
// them routes Err through the single errMarker, so there is one redaction
// decision rather than five. This is the same four-method redaction pattern
// data-model.md defines for the credential type, plus Formatter. config.Secret
// needs Formatter too — an earlier version of this comment said it did not,
// on the reasoning that the credential type has no exported fields to dump,
// which is wrong: fmt reaches unexported fields through reflection, so %d on a
// bare Secret rendered the token. Err stays exported so the diagnostic logger
// can still reach it deliberately as r.Err — the guards stop accidental
// renders, not intentional ones.
//
// Hold a SinkResult as a named field, never embedded. The guards are promoted
// along with the fields, so an embedding struct would render and marshal as a
// bare SinkResult and silently lose every field of its own.
//
// The JSON form is write-only by design: Err marshals to a marker, so a result
// cannot round-trip, and there is deliberately no UnmarshalJSON to suggest it
// can. It is also not the diagnostic record's shape — log-events.md and FR-066
// want flat snake_case keys and duration_ms — so a log record must be assembled
// field by field (T021-T024) rather than by handing a SinkResult to slog.Any or
// json.Marshal as the record body. Whatever assembles it has to serialize r.Err
// itself, since no render of the result will: FR-017's diagnostic half lives
// only in that deliberate access.
type SinkResult struct {
	// Name is the sink's name, matching Sink.Name.
	Name string

	// Success reports whether the message reached this sink's destination. A
	// chat delivery rescued by the unformatted retry is a success (FR-061).
	Success bool

	// Reason is a short, safe, human-readable phrase for display. Empty on
	// success.
	Reason string

	// Err is the detailed diagnostic error, for the log only. Never shown to
	// the user, and never the source of Reason.
	Err error

	// Duration is how long the sink took, recorded as duration_ms (FR-066).
	Duration time.Duration
}

// Markers printed in place of Err by every guarded render.
//
// The two are distinct because whether a diagnostic exists is itself useful and
// costs nothing to reveal: someone reading a failure line needs to know there is
// something in the log worth looking up, and that single bit says nothing about
// the content. Collapsing both cases into one marker would make an empty result
// and a token-bearing transport error look identical.
const (
	errRedactedMarker = "[redacted]"
	errAbsentMarker   = "[none]"
)

// errMarker reports whether a diagnostic error is present, without any of its
// content.
//
// It exists so the redaction decision is made in exactly one place. Five
// renders each deciding for themselves is five chances for one of them to drift
// into printing the error, and the drift would be invisible until a token showed
// up in a user's terminal.
//
// An Err holding a typed nil — a (*url.Error)(nil) stored in the interface — is
// reported as present, because the interface itself is not nil. That claims a
// diagnostic exists when none does, which is the harmless direction of the two
// mistakes available here: it over-redacts and never discloses. It is left as
// is rather than defended with reflection, because the same value panics on the
// r.Err.Error() the diagnostic logger performs, so a typed nil is a bug at the
// site that builds the result and hiding it here would only delay the diagnosis.
func (r SinkResult) errMarker() string {
	if r.Err == nil {
		return errAbsentMarker
	}

	return errRedactedMarker
}

// String renders the safe fields and replaces Err with a marker. It backs the
// %v, %s and %+v verbs through Format, and every implicit stringification
// (print, log, string concatenation through fmt) directly.
//
// The receiver is a value, not a pointer, so both a SinkResult and a
// *SinkResult are covered; a pointer method would leave the far more common
// value form rendering its raw fields. The format string names the fields
// individually and never formats r itself, which both avoids infinite recursion
// through fmt and makes it impossible to add a field later that is printed
// without someone deciding it is safe.
func (r SinkResult) String() string {
	return fmt.Sprintf("{Name:%s Success:%t Reason:%s Err:%s Duration:%s}",
		r.Name, r.Success, r.Reason, r.errMarker(), r.Duration)
}

// GoString backs %#v, which ignores String and would otherwise print the
// struct's fields directly.
//
// The output deliberately is not compilable Go, as %#v normally promises: the
// whole point is that the error is not reproduced, so no rendering of it can be
// faithful. A marker that reads as redacted is more useful to whoever is staring
// at the output than syntax that could be pasted back into a source file.
func (r SinkResult) GoString() string {
	return fmt.Sprintf("post.SinkResult{Name:%q, Success:%t, Reason:%q, Err:%q, Duration:%d}",
		r.Name, r.Success, r.Reason, r.errMarker(), r.Duration)
}

// Format is what actually closes the fmt surface, because String and GoString
// between them do not.
//
// fmt consults Stringer for only v, s, q, x and X, and GoStringer for %#v.
// Every other verb — %t, %d, %c, anything a typo produces — falls through to
// fmt's bad-verb path, which suppresses method dispatch and dumps the exported
// fields, walking straight into Err and printing the token String was added to
// hide. fmt.Formatter outranks both Stringer and GoStringer for every verb, so
// implementing it once covers all of them. String and GoString stay: they are
// still the right implementations, this method delegates to them, and other
// code may call them directly.
//
// Three verbs stay out of reach, because fmt answers them before it consults
// Formatter: %T, which prints the type name and never touches the value, and %p
// and %w, which fmt rejects outright on an operand that is neither a pointer
// nor an error and then dumps the fields with method dispatch suppressed. That
// dump reaches Err, but Err is an interface holding a pointer in every real
// case and fmt prints a nested pointer as an address, so the token does not
// escape — an error stored as a struct value rather than a pointer would have
// its own fields dumped. All three are programming errors on a SinkResult
// anyway; the leak test sweeps the whole alphabet so a change here is visible.
//
// Width and precision are ignored. They are padding for a display string, and a
// SinkResult is not one.
func (r SinkResult) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('#') {
			fmt.Fprint(f, r.GoString())

			return
		}

		// %+v and %v are the same text. The struct has no field worth
		// expanding differently, and the one field that would differ is the
		// one being withheld.
		fmt.Fprint(f, r.String())
	case 's':
		fmt.Fprint(f, r.String())
	case 'q':
		fmt.Fprintf(f, "%q", r.String())
	default:
		// fmt's own wrong-verb shape, so the output still reads as the mistake
		// it is, with String's already-redacted text standing in for the dump
		// fmt would otherwise produce.
		fmt.Fprintf(f, "%%!%c(post.SinkResult=%s)", verb, r.String())
	}
}

// MarshalJSON covers encoding/json, which consults none of the fmt interfaces.
// A value receiver means a []SinkResult — the shape a front door or a log record
// actually carries — is covered as well as a single result.
//
// The field names match what the default marshaller produced before this method
// existed, so nothing downstream has to know the guard is here. Err's JSON type
// does change, from an object or null to a string: the default marshaller never
// emitted the error's text, it emitted the error value's own exported fields,
// which for the *url.Error of a Telegram transport failure is exactly where the
// token was. A consumer reading Err as an object was reading the leak.
//
// The shadow struct types Err as a string rather than aliasing SinkResult, so
// the recursion that an alias-free re-marshal of the same type would cause
// cannot happen.
func (r SinkResult) MarshalJSON() ([]byte, error) {
	safe := struct {
		Name     string
		Success  bool
		Reason   string
		Err      string
		Duration time.Duration
	}{
		Name:     r.Name,
		Success:  r.Success,
		Reason:   r.Reason,
		Err:      r.errMarker(),
		Duration: r.Duration,
	}

	return json.Marshal(safe)
}

// LogValue covers log/slog, which consults neither String, GoString nor
// MarshalJSON for an attribute whose value implements slog.LogValuer —
// LogValuer outranks both. Without this method a result passed to slog.Any, or
// as a bare key/value pair, is resolved by the handler's own formatting and
// reaches Err. That matters most here of anywhere: the destination is the
// rotating on-disk log, so the leak would be written down and kept.
//
// A group rather than a string, so a handler can index the safe fields
// individually. The keys are slog-conventional lowercase and are deliberately
// not the diagnostic record's schema; see the type comment.
func (r SinkResult) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", r.Name),
		slog.Bool("success", r.Success),
		slog.String("reason", r.Reason),
		slog.String("err", r.errMarker()),
		slog.Duration("duration", r.Duration),
	)
}

// AllSucceeded reports whether the post as a whole succeeded, which is true
// only when every enabled sink succeeded (FR-059). The process exits 0 on true
// and 1 on false (FR-060).
//
// Only enabled sinks produce results, so a disabled sink cannot make this
// false (FR-016).
//
// An empty slice reports false rather than the vacuous true that an "all"
// predicate would normally give. Nothing was delivered, so nothing succeeded,
// and reporting success would exit 0 for a post that reached no destination —
// the one outcome the exit status exists to make visible. FR-018 makes settings
// with every sink disabled a startup error, so the orchestrator should never
// reach here with no results; this is the fail-closed behavior for the case
// where some future path does.
func AllSucceeded(results []SinkResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if !result.Success {
			return false
		}
	}

	return true
}
