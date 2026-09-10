package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// Record keys this adapter writes (contracts/log-events.md, FR-066).
//
// The preamble keys — ts, level, event, source, message_id, app_version,
// git_commit — belong to internal/logging and are named there. These are the
// rest of the field vocabulary, named here because this is the only code that
// writes them, so a typo is one grep away from the contract table rather than
// spread across a dozen call sites.
//
// None of them collides with a key internal/logging reserves. That is asserted
// rather than assumed: a caller attribute keyed with a reserved name is dropped
// silently by the emission path, so a collision would not fail, it would make a
// required field quietly absent.
const (
	keySink         = "sink"
	keyPath         = "path"
	keyDurationMS   = "duration_ms"
	keyError        = "error"
	keyErrorType    = "error_type"
	keyHTTPStatus   = "http_status"
	keyMessageLen   = "message_len"
	keyMessageBytes = "message_bytes"
)

// lifecycle is one sink's three event names.
type lifecycle struct {
	started   logging.Event
	succeeded logging.Event
	failed    logging.Event
}

// sinkLifecycle pairs a sink's name with the events its lifecycle produces.
type sinkLifecycle struct {
	sink   string
	events lifecycle
}

// recordedSinks returns every sink this adapter has event names for.
//
// One source for both the lookup and the completeness check. The event
// vocabulary is keyed by sink, and internal/logging's own registration test
// scans only its own source — it cannot see this file, so a name it defines and
// this adapter never produces would be a query that silently returns nothing
// forever. TestTheAdapterProducesEveryOrchestratorReachableEvent closes that by
// deriving the producible set from here and comparing it against
// logging.AllEvents (decision DEC-D2).
//
// A function rather than a package-level map for the reason logging.AllEvents is
// a function: a var would be writable by any importer, and one stray assignment
// would corrupt a vocabulary the whole process shares. Two entries scanned
// linearly per record is not a cost worth a package-level map.
func recordedSinks() []sinkLifecycle {
	return []sinkLifecycle{
		{
			sink: obsidian.SinkName,
			events: lifecycle{
				started:   logging.EventObsidianAppendStarted,
				succeeded: logging.EventObsidianAppendSucceeded,
				failed:    logging.EventObsidianAppendFailed,
			},
		},
		{
			sink: telegram.SinkName,
			events: lifecycle{
				started:   logging.EventTelegramSendStarted,
				succeeded: logging.EventTelegramSendSucceeded,
				failed:    logging.EventTelegramSendFailed,
			},
		},
	}
}

// lifecycleFor returns the event names for a sink, and false when there are
// none.
//
// False is reachable in production, which is why it is a returned value rather
// than a panic or a silent zero. post.Service resolves a sink whose Name panics
// or returns "" to a sentinel, so a real run can hand this a name no vocabulary
// covers; and a third sink added without extending recordedSinks would arrive
// here too. Both are bugs, and neither may cost the post its records silently —
// see recorder.note.
func lifecycleFor(sink string) (lifecycle, bool) {
	for _, entry := range recordedSinks() {
		if entry.sink == sink {
			return entry.events, true
		}
	}

	return lifecycle{}, false
}

// producibleEvents returns every event name this adapter can emit.
//
// Derived from recordedSinks rather than listed, so adding a sink cannot leave
// the completeness check behind.
func producibleEvents() []logging.Event {
	events := make([]logging.Event, 0, 2+3*len(recordedSinks()))

	events = append(events, logging.EventMessageReceived)

	for _, entry := range recordedSinks() {
		events = append(events, entry.events.started, entry.events.succeeded, entry.events.failed)
	}

	return append(events, logging.EventRequestCompleted, logging.EventRequestCompletedWithError)
}

// Recording adapts the diagnostic logger to post.Recording (T040, DEC-D2).
//
// This type is the mapping internal/post deliberately does not contain: the
// posting core reports a lifecycle in its own terms, and the translation into
// contracts/log-events.md's event names and field vocabulary happens here,
// where importing internal/logging and both sink packages is allowed. It is
// also where the fidelity has to be proved — the traps on issue #41's list are
// all record-shape bugs, so this adapter's tests read bytes out of a real
// logging.Logger rather than counting calls on a spy.
type Recording struct {
	logger *logging.Logger
}

// NewRecording returns the Recording for one run, or one that records nothing
// when logger is nil.
//
// It does not take Options and does not open anything: OpenLogger already owns
// the construction, including the Redact wiring that keeps the bot token out of
// every record (issue #41). Splitting them keeps that obligation in one place —
// a Recording built from a logger someone assembled without Redact would scrub
// nothing, and nothing in the records would say so.
func NewRecording(logger *logging.Logger) *Recording {
	return &Recording{logger: logger}
}

// Post returns the recorder for one post (post.Recording).
//
// Returns an untyped nil when there is no logger, which post.New tolerates as
// "record nothing". Explicitly untyped: a nil *recorder returned as a
// post.Recorder is not a nil interface, so it would pass the orchestrator's nil
// check and then be caught only by its panic guard — safe, but it would spend a
// guard on a case that can be expressed exactly.
func (r *Recording) Post(id string) post.Recorder {
	if r == nil || r.logger == nil {
		return nil
	}

	return &recorder{log: r.logger.Post(id)}
}

// recorder emits one post's records.
//
// Concurrent by construction: sink events arrive on the delivery goroutines.
// logging.PostLogger is safe for that, so the only shared state needing a lock
// is the note list below.
type recorder struct {
	log *logging.PostLogger

	// mu guards notes.
	mu sync.Mutex

	// notes collects diagnostics that no lifecycle record can carry, for the
	// post's terminal record.
	//
	// Two things land here, and both are bugs in a sink rather than in a post.
	// A panicking Name has no record of its own to ride on, because the event
	// vocabulary is keyed by sink name and a panicking Name resolves to a
	// sentinel that no vocabulary covers (issue #110). And a sink name with no
	// registered events cannot produce lifecycle records at all. The terminal
	// record is the one record that is always emitted, so putting them there is
	// the difference between a diagnostic that is awkwardly placed and one that
	// does not exist.
	notes []string
}

// MessageReceived records the post entering the system (post.Recorder).
//
// The two counts are FR-066's message_len and message_bytes, as R-010 defines
// them: a rune count and a UTF-8 byte length. They are the whole payload,
// because the message *body* is deliberately absent — FR-068's capture rule
// (record the body whenever any enabled sink failed, so the post can be
// reconstructed and re-sent by hand) is T071, and it needs the
// message_on_error_only setting and knowledge of the post's outcome, neither of
// which exists at this point in a post. So this batch knowingly leaves FR-068
// unmet rather than emitting a body unconditionally, which would leak the user's
// private notes into the log on every successful post and violate the same rule
// from the other side.
//
// The counts themselves are exact rather than approximate, and the distinction
// is the point of having both: len is bytes, and a rune count needs
// utf8.RuneCountInString because a range loop counts runes but len counts bytes,
// and for the CJK text this program was written for the two differ by a factor
// of three. Invalid UTF-8 cannot reach here — post.Message.Validate refuses it
// (DEC-D4) — so RuneCountInString's substitution behaviour is not in play.
func (r *recorder) MessageReceived(message post.Message) {
	r.log.Info(logging.EventMessageReceived,
		slog.Int(keyMessageLen, utf8.RuneCountInString(message.Original)),
		slog.Int(keyMessageBytes, len(message.Original)),
	)
}

// SinkStarted records one sink beginning its delivery (post.Recorder).
//
// This is where a Name panic is collected, and only here: the orchestrator
// guarantees a start precedes the matching finish for every sink, so collecting
// it in one of the two rather than both records it exactly once without needing
// a second dedupe.
func (r *recorder) SinkStarted(attempt post.SinkAttempt) {
	if attempt.NameErr != nil {
		r.note(fmt.Sprintf("sink %s: obtaining the sink's name panicked: %s",
			attempt.Sink, errorMessage(attempt.NameErr)))
	}

	events, ok := lifecycleFor(attempt.Sink)
	if !ok {
		r.note(fmt.Sprintf("sink %s: no diagnostic events are registered for this name, "+
			"so its start and outcome were not recorded", attempt.Sink))

		return
	}

	r.log.Info(events.started, sinkAttrs(attempt)...)
}

// SinkFinished records one sink's outcome (post.Recorder).
//
// The unregistered-name case emits nothing and adds no note, because
// SinkStarted already added one covering both stages: a second would put the
// same sentence twice into one record.
func (r *recorder) SinkFinished(attempt post.SinkAttempt, result post.SinkResult) {
	events, ok := lifecycleFor(attempt.Sink)
	if !ok {
		return
	}

	attrs := sinkAttrs(attempt)

	// Int64 of Milliseconds, never slog.Duration. slog's JSON handler renders
	// a Duration as its nanosecond count, so slog.Duration(keyDurationMS, d)
	// emits nanoseconds under a key promising milliseconds — a factor of a
	// million, in a field FR-066 requires, with nothing in the record to make
	// it look wrong (issue #41).
	attrs = append(attrs, slog.Int64(keyDurationMS, result.Duration.Milliseconds()))

	if result.Success {
		r.log.Info(events.succeeded, attrs...)

		return
	}

	// error_type and error are both required on a failure and are deliberately
	// different things: the type is a short slug consumers group by, chosen
	// from constants in internal/post, and the message is the detailed
	// diagnostic. Reason is in neither, because Reason is the display half of
	// FR-017 and the log wants the half a front door must not print.
	attrs = append(attrs,
		slog.String(keyErrorType, post.ErrorType(result)),
		slog.String(keyError, errorMessage(result.Err)),
	)

	// The one field that comes from a sink's own error type. errors.As rather
	// than a string match, and it walks the chain deliberately: the telegram
	// sink wraps a leaking error in its own redacting type, and the status is
	// on the *APIError underneath.
	if status, ok := httpStatus(result.Err); ok {
		attrs = append(attrs, slog.Int(keyHTTPStatus, status))
	}

	r.log.Error(events.failed, attrs...)
}

// PostCompleted records the post's terminal event (post.Recorder).
//
// The name and the level describe the post: which of the two names is emitted
// tracks AllSucceeded, and therefore the exit status (FR-059, FR-060). A note
// collected during the post does not change either of those, even though it is
// carried in the error field — it is a diagnostic about a sink's *code*, not
// about whether the message was delivered, and reporting a delivered post as
// failed because a sink's Name method misbehaved would be the wrong answer to
// the wrong question.
func (r *recorder) PostCompleted(outcome post.Outcome, elapsed time.Duration) {
	attrs := make([]slog.Attr, 0, 2)
	attrs = append(attrs, slog.Int64(keyDurationMS, elapsed.Milliseconds()))

	if notes := r.takeNotes(); notes != "" {
		attrs = append(attrs, slog.String(keyError, notes))
	}

	if outcome.Succeeded() {
		r.log.Info(logging.EventRequestCompleted, attrs...)

		return
	}

	r.log.Error(logging.EventRequestCompletedWithError, attrs...)
}

// note collects a diagnostic for the terminal record.
func (r *recorder) note(diagnostic string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.notes = append(r.notes, diagnostic)
}

// takeNotes returns the collected diagnostics as one field value, and empties
// the list.
//
// Joined rather than emitted as several attributes because they share one key,
// and slog does not deduplicate: two error attributes would produce one object
// with the key twice, which parses and then silently gives every consumer
// whichever copy its decoder happened to keep.
//
// Emptied so that a Recording reused across posts — which nothing does today,
// since Recording.Post builds a fresh recorder — cannot carry one post's
// diagnostic onto the next post's record.
func (r *recorder) takeNotes() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.notes) == 0 {
		return ""
	}

	joined := strings.Join(r.notes, "; ")
	r.notes = nil

	return joined
}

// sinkAttrs builds the identity fields every one of a sink's records carries.
//
// path appears only when the sink reported a destination, which is what keeps it
// off the chat events (issue #98): the telegram sink reports nothing, so its
// records have no path field at all rather than an empty one. An empty string
// would satisfy the field's presence and mean nothing, and a consumer filtering
// on path would match records for a destination that has none.
func sinkAttrs(attempt post.SinkAttempt) []slog.Attr {
	attrs := make([]slog.Attr, 0, 4)

	attrs = append(attrs, slog.String(keySink, attempt.Sink))

	if attempt.Target != "" {
		attrs = append(attrs, slog.String(keyPath, attempt.Target))
	}

	return attrs
}

// errorMessage renders an error's message without letting the error's own
// rendering take down the post.
//
// Error() is the deliberate access FR-017 reserves for the diagnostic log, and
// it is a method on a value this program did not write. Two shapes make it
// dangerous. A typed nil stored in the interface — `return (*myError)(nil)` from
// a Send — is non-nil as an interface, so the orchestrator builds a failure
// result around it and Error() then dereferences nothing; internal/post's own
// comment on SinkResult records that this panics at exactly this call. And a
// sink's error type may simply have a faulty Error().
//
// Guarding here rather than letting slog do it is the difference between losing
// one field and losing the log. Handing the error over as slog.Any would move
// the panic inside logging.PostLogger, which recovers it — but that recovery
// latches a degradation, which spends FR-076's single warning and discards the
// whole record. Calling Error() unguarded would be worse still: the panic would
// unwind the delivery goroutine and take the post with it.
//
// %T on the value reads the type and never the value, so it is safe on exactly
// the input that made the render fail. This mirrors post.describePanic, which
// solves the same problem for a recovered panic value.
func errorMessage(err error) (message string) {
	if err == nil {
		// Not reachable through internal/post's orchestrator, which never
		// builds a failure without an error. A required field with a sentinel
		// beats a required field that is silently absent.
		return "the sink reported a failure without a diagnostic error"
	}

	defer func() {
		if recover() != nil {
			message = fmt.Sprintf("an error of type %T whose own rendering panicked", err)
		}
	}()

	return err.Error()
}

// httpStatus reports the HTTP status a chat failure carried, when it carried
// one.
//
// contracts/log-events.md puts http_status on "Telegram failures with a
// response", and *telegram.APIError is the only shape that has one: the sink
// constructs it for every reply that arrived, including replies whose body could
// not be decoded, which is why its own comment calls HTTPStatus the field T040
// must log. A transport failure never reached a response and correctly has no
// status.
//
// The nil check after errors.As is not redundant. errors.As reports true for a
// typed nil in the chain and leaves the target nil, which would then panic on
// the field access — the same shape errorMessage guards.
func httpStatus(err error) (int, bool) {
	var apiErr *telegram.APIError

	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.HTTPStatus, true
	}

	return 0, false
}
