package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// redactedMarker stands in for a credential in every render of a Secret.
//
// One constant rather than a literal per method, so the guards below cannot
// drift apart and a test can assert against the same value the code emits.
const redactedMarker = "[redacted]"

// Secret holds the Telegram bot token.
//
// FR-043 says the credential must never appear in logs, on-screen errors,
// command-line errors, or settings dumps, and FR-069 says secrets must never be
// recorded. Neither is a rule a call site can be trusted to remember: the token
// is reachable from Settings, and Settings is exactly the kind of value someone
// prints while debugging. So the guarantee is built into the type. The value is
// unexported and every route out of it except Reveal returns the marker.
//
// The four-method redaction pattern (String, GoString, MarshalJSON, LogValue)
// is what data-model.md prescribes, and it is the same pattern SinkResult uses.
// Format is the fifth, and it is not decorative — see its comment. The comment
// on post.SinkResult claims the credential type does not need Format "because
// it has no exported fields to dump"; that reasoning is wrong, and this type
// carries the correction rather than inheriting the gap.
//
// There is deliberately no MarshalText and no MarshalTOML. v0.1 never writes a
// settings file back out, and the absence means a future round-trip has to add
// the method — and decide what it should emit — instead of silently inheriting
// one that works.
type Secret struct {
	// value is a pointer, and that is load bearing rather than idiomatic
	// preference.
	//
	// Unexported is not the same as unprintable: fmt reaches unexported fields
	// through reflection when it dumps a struct, and it cannot call a method
	// on one, so no guard on this type can intercept that dump. Two verbs
	// reach it — %p on a non-pointer and %w outside fmt.Errorf — because fmt
	// answers both before it consults Formatter. Held as a string, they print
	// the credential; held as a pointer, they print an address, because fmt
	// renders a nested pointer as its address rather than following it. That
	// is the same mechanism post.SinkResult relies on to keep its Err out of
	// the equivalent dump, and it is what makes the redaction total instead of
	// total-except-two-verbs. The test sweeps the whole alphabet in both
	// cases, so a change back to a plain string fails visibly.
	//
	// A nil pointer is the empty credential, which is what the zero Secret and
	// an unconfigured settings file both produce. Every method below handles
	// it, so a zero Secret is usable rather than a panic waiting to happen.
	//
	// Copying a Secret copies the pointer, so two copies alias one string.
	// That is safe because a Secret is immutable after construction: nothing
	// writes through the pointer, and UnmarshalText installs a new one rather
	// than assigning through the old.
	value *string
}

// NewSecret wraps a credential. The plain string should not outlive the call.
func NewSecret(value string) Secret {
	return Secret{value: &value}
}

// Reveal returns the real credential.
//
// This is the single deliberate way out of the type, and it is meant to be
// called at exactly one place: building the Telegram request URL (T033). It is
// named Reveal rather than Value or String so that a call to it reads as a
// decision at the call site and stands out in review.
func (s Secret) Reveal() string {
	if s.value == nil {
		return ""
	}

	return *s.value
}

// Len reports the length of the secret in bytes without exposing it.
//
// It exists so a validation rule about the credential's *shape* does not have to
// become another Reveal() call site. The count of routes to the real value is a
// review obligation in this project (see data-model.md), and "is this token long
// enough to be safe as a redaction pattern?" is a question that can be answered
// without answering "what is the token?".
func (s Secret) Len() int {
	if s.value == nil {
		return 0
	}

	return len(*s.value)
}

// IsEmpty reports whether any credential is held.
//
// Validation needs to know whether the token is present without looking at it,
// so this exists rather than having a caller compare Reveal() to "" — which
// would put the plain value in a caller's expression for no reason.
func (s Secret) IsEmpty() bool {
	return s.value == nil || *s.value == ""
}

// UnmarshalText lets go-toml decode a bare string key into the type.
//
// go-toml/v2 honours encoding.TextUnmarshaler, so `bot_token = "..."` lands
// here rather than needing an intermediate string field on the settings struct.
// That matters: an intermediate field would be an exported plain string on
// Settings for the lifetime of the load, which is exactly the exposure this
// type exists to remove.
//
// The pointer receiver is required for the decoder to find the method; the
// decoder is handed an addressable struct field, so this is satisfied.
func (s *Secret) UnmarshalText(text []byte) error {
	// A new string and a new pointer, never a write through the existing one:
	// see the aliasing note on the field.
	value := string(text)
	s.value = &value

	return nil
}

// String backs %v, %s and %q through Format, and every implicit
// stringification (print, log, string concatenation through fmt) directly.
//
// The receiver is a value so both Secret and *Secret are covered.
func (s Secret) String() string {
	return redactedMarker
}

// GoString backs %#v, which ignores String and would otherwise print the
// struct's fields — including the unexported one. Verified: without this
// method, %#v on a Secret renders config.Secret{value:"<the token>"}.
//
// The output is deliberately not compilable Go, as %#v normally promises. The
// point is that the credential is not reproduced, so no faithful rendering
// exists.
func (s Secret) GoString() string {
	return "config.Secret{" + redactedMarker + "}"
}

// Format is what actually closes the fmt surface, and on this type it is load
// bearing rather than defensive.
//
// fmt consults Stringer for only v, s, q, x and X, and GoStringer for %#v.
// Every other verb falls through to fmt's bad-verb path, which suppresses
// method dispatch and dumps the struct's fields through reflection. Reflection
// reads unexported fields, so with String alone `%d` on a Secret renders
// {%!d(string=<the token>)} — and the same holds for a Secret nested inside
// Settings, because fmt applies the same dispatch to each field it walks. That
// makes a single mistyped verb anywhere in the program a credential disclosure.
// fmt.Formatter outranks both Stringer and GoStringer for every verb, so
// implementing it once covers all of them.
//
// Two verbs stay out of reach even so: %p on a non-pointer operand, and %w
// outside fmt.Errorf. fmt rejects both before consulting Formatter and then
// dumps the fields with dispatch suppressed. Neither can be intercepted by any
// method, which is why the field is a pointer — the dump then yields an address
// rather than the credential. Both are covered by the leak sweep in this
// package's test. %T is answered before Formatter too, but it never reads the
// value.
//
// Width and precision are ignored. They are padding for a display string, and a
// credential is not one.
func (s Secret) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('#') {
			fmt.Fprint(f, s.GoString())

			return
		}

		fmt.Fprint(f, s.String())
	case 's':
		fmt.Fprint(f, s.String())
	case 'q':
		fmt.Fprintf(f, "%q", s.String())
	default:
		// fmt's own wrong-verb shape, so the output still reads as the mistake
		// it is, with the marker standing in for the dump fmt would produce.
		fmt.Fprintf(f, "%%!%c(config.Secret=%s)", verb, s.String())
	}
}

// MarshalJSON covers encoding/json, which consults none of the fmt interfaces.
// A value receiver means a Settings containing a Secret is covered as well as a
// bare Secret.
//
// There is deliberately no UnmarshalJSON: the marker cannot round-trip back
// into a credential, and offering the method would imply it can.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(redactedMarker)
}

// LogValue covers log/slog, which consults neither String, GoString nor
// MarshalJSON for a value implementing slog.LogValuer — LogValuer outranks all
// three. This matters most of anywhere: the destination is the rotating on-disk
// log (T023), so an unguarded render would be written down and kept.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(redactedMarker)
}
