package app_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// errSendFailed is an ordinary sink failure: the shape FR-071 calls an expected
// operational error, carrying no trace of its own.
var errSendFailed = errors.New("read-only file system")

// readLog returns the log's raw bytes as text, for the assertions that are
// about what is absent from the whole file rather than about one field.
func readLog(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}

	return string(raw)
}

// T066 and T073: FR-068's message-capture rule and FR-071's traces.
//
// These read the records off disk through postThrough, for the reason recorded
// at the top of recorder_test.go: every defect worth fearing here is a
// record-shape defect — a field present when the rule says omit it, absent when
// the rule says record it, or carrying a credential — and none of those is
// visible to a spy that counts calls.

// panickingSink panics inside Send, which is the shape FR-071 wants a trace
// for. The panic text is distinctive so a test can prove the trace is this
// goroutine's and not a plausible-looking string.
type panickingSink struct{}

func (s *panickingSink) Name() string { return telegram.SinkName }

func (s *panickingSink) Send(context.Context, post.Message) error {
	panic("send exploded in a distinctive way")
}

// fieldOf returns one record's field by event name, and whether the record had
// it at all.
//
// Presence is returned rather than folded into a zero value because every
// assertion in this file is about presence or absence: FR-068 says a
// successful record must *omit* the body, and an empty string would satisfy a
// naive check while still putting a `message` key in front of every consumer.
func fieldOf(t *testing.T, records []map[string]any, event, key string) (any, bool) {
	t.Helper()

	matches := 0

	var (
		value any
		found bool
	)

	for _, record := range records {
		if record["event"] == event {
			matches++
			value, found = record[key]
		}
	}

	switch matches {
	case 0:
		t.Fatalf("no %q record in %d records", event, len(records))
	case 1:
	default:
		// Loud rather than silent. An earlier version returned on the first
		// match, so a post with two records of one event name — two sinks
		// resolving to the same registered name, or an event that starts
		// firing twice — would have been half-checked, with the assertion
		// still passing on whichever record happened to come first.
		t.Fatalf("%d %q records; this helper asserts about one, so it cannot speak for this post",
			matches, event)
	}

	return value, found
}

// recordsWith returns every record that has the key.
func recordsWith(records []map[string]any, key string) []map[string]any {
	var found []map[string]any

	for _, record := range records {
		if _, ok := record[key]; ok {
			found = append(found, record)
		}
	}

	return found
}

// TestAFullySuccessfulPostRecordsNoMessageBody is T066 and FR-068's first half.
//
// The assertion is over *every* record rather than over the terminal one,
// because the rule is that no record carries the body, and the natural
// implementation bug puts it on the intake event where the body is first
// available.
func TestAFullySuccessfulPostRecordsNoMessageBody(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &noteSink{note: "today.md"}, &chatSink{})

	if carrying := recordsWith(records, "message"); len(carrying) != 0 {
		t.Errorf("a fully successful post put the body on %d record(s); FR-068 requires none", len(carrying))

		for _, record := range carrying {
			t.Errorf("  event %v carried message=%q", record["event"], record["message"])
		}
	}

	// The counts stay: FR-068 permits a recorded length on a successful post,
	// and this is the half of the rule that a fix for the half above could
	// break by dropping the intake fields altogether.
	if _, ok := fieldOf(t, records, "message_received", "message_len"); !ok {
		t.Error("message_received lost message_len; FR-068 permits the length on a successful post")
	}
}

// TestAFailedPostRecordsTheBodyOnTheRecordThatNamesTheFailure is T066, FR-068's
// second half and SC-008.
//
// SC-008 asks for enough in the log to re-send the post by hand *without
// consulting any other source*, so the assertion is that one single record
// carries all four things it names: the original message, the destination, the
// error type and the detail. A body on the terminal record and the error on a
// different one would satisfy a per-field check and still make a human join two
// lines to recover the post.
func TestAFailedPostRecordsTheBodyOnTheRecordThatNamesTheFailure(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path,
		&noteSink{note: "today.md"},
		&chatSink{sendErr: errSendFailed},
	)

	var failure map[string]any

	for _, record := range records {
		if record["event"] == "telegram_send_failed" {
			failure = record
		}
	}

	if failure == nil {
		t.Fatal("no telegram_send_failed record")
	}

	for _, key := range []string{"message", "sink", "error_type", "error"} {
		if _, ok := failure[key]; !ok {
			t.Errorf("the failure record has no %q; SC-008 wants the post re-sendable from this one record", key)
		}
	}

	if got := failure["message"]; got != "記録される投稿" {
		t.Errorf("the captured body is %q, want the original message", got)
	}
}

// TestBothFailuresCarryTheBody is FR-070 together with FR-068.
//
// FR-070 requires every failure to be logged, and SC-008 wants each failure
// record self-sufficient. The bug this exists for is a capture that fires once:
// a flag consumed by the first failure leaves the second record naming a
// destination and an error with no body beside it, which is exactly the record
// a reader reaches for when the *second* destination is the one they must
// re-send to by hand.
func TestBothFailuresCarryTheBody(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path,
		&noteSink{note: "today.md", sendErr: errSendFailed},
		&chatSink{sendErr: errSendFailed},
	)

	for _, event := range []string{"obsidian_append_failed", "telegram_send_failed"} {
		body, ok := fieldOf(t, records, event, "message")
		if !ok {
			t.Errorf("%s carries no message; FR-070 logs every failure and SC-008 wants each one re-sendable", event)

			continue
		}

		if body != "記録される投稿" {
			t.Errorf("%s captured %q, want the original message", event, body)
		}
	}
}

// TestTheTerminalRecordCarriesTheBodyWhenNoFailureRecordCan covers the one
// failure shape that produces no per-sink record at all.
//
// A sink whose Name panics resolves to a sentinel the event vocabulary does not
// cover (issue #110), so SinkStarted returns early and neither of its lifecycle
// records is emitted. The terminal record is then the post's only record, and
// without a fallback FR-068 would be unmet for exactly this post — a failed
// post whose body is nowhere.
func TestTheTerminalRecordCarriesTheBodyWhenNoFailureRecordCan(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &namelessSink{sendErr: errSendFailed})

	body, ok := fieldOf(t, records, "request_completed_with_error", "message")
	if !ok {
		t.Fatal("the terminal record carries no message, and no other record could; FR-068 is unmet for this post")
	}

	if body != "記録される投稿" {
		t.Errorf("the terminal record captured %q, want the original message", body)
	}
}

// TestTheTerminalRecordDoesNotRepeatABodyAFailureRecordAlreadyCarried is the
// other half of the claim, and the reason it is a claim rather than a plain
// read.
//
// Without it the body lands on every failure record *and* on the terminal one,
// which for a two-sink post that lost both destinations puts the user's private
// text into the log three times.
func TestTheTerminalRecordDoesNotRepeatABodyAFailureRecordAlreadyCarried(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path,
		&noteSink{note: "today.md"},
		&chatSink{sendErr: errSendFailed},
	)

	if _, ok := fieldOf(t, records, "request_completed_with_error", "message"); ok {
		t.Error("the terminal record repeated a body that telegram_send_failed already carried")
	}

	if carrying := recordsWith(records, "message"); len(carrying) != 1 {
		t.Errorf("the body appears on %d records, want exactly the one that names the failure", len(carrying))
	}
}

// TestMessageOnErrorOnlyOffRecordsTheBodyOnIntakeAndNowhereElse pins the
// setting's other position.
//
// FR-068 constrains only the enabled case, so the disabled case is a decision:
// the body goes on the intake event, which happens once per post, rather than
// onto every record, which would repeat the user's text once per destination
// for no reconstruction benefit. The assertion is both halves — present on
// message_received, and on nothing else — because a change that starts
// emitting it per-record would still pass a test that only looked at intake.
func TestMessageOnErrorOnlyOffRecordsTheBodyOnIntakeAndNowhereElse(t *testing.T) {
	settings, path := loggedSettings(t)
	settings.Logging.MessageOnErrorOnly = false

	records := postThrough(t, settings, path,
		&noteSink{note: "today.md"},
		&chatSink{sendErr: errSendFailed},
	)

	body, ok := fieldOf(t, records, "message_received", "message")
	if !ok {
		t.Fatal("with message_on_error_only off, message_received carries no body")
	}

	if body != "記録される投稿" {
		t.Errorf("message_received captured %q, want the original message", body)
	}

	if carrying := recordsWith(records, "message"); len(carrying) != 1 {
		t.Errorf("the body appears on %d records, want only message_received", len(carrying))

		for _, record := range carrying {
			t.Errorf("  event %v carried the body", record["event"])
		}
	}
}

// TestTheShippedDefaultsRestrictCaptureAndCollectTraces pins the shipped
// defaults, which NewRecording's comment relies on.
//
// Named for what it asserts — the two settings' shipped values — rather than
// for the record-level consequence, which is
// TestAFullySuccessfulPostRecordsNoMessageBody's job. A name promising a
// property this body does not check is how a future reader concludes the
// shipped-default path is covered when it is not.
//
// The zero value of config.LoggingSettings has MessageOnErrorOnly false, which
// means "record the body always" — the less private of the two positions. That
// is safe only because Defaults sets it true and both front doors build the
// Recording from loaded settings. This is that assumption, asserted.
func TestTheShippedDefaultsRestrictCaptureAndCollectTraces(t *testing.T) {
	if !config.Defaults().Logging.MessageOnErrorOnly {
		t.Error("the shipped default no longer restricts message capture to failures; " +
			"NewRecording's zero-value reasoning and FR-068's privacy default both rest on this")
	}

	if !config.Defaults().Logging.StackTrace {
		t.Error("the shipped default no longer collects traces; FR-071's traces would be absent by default")
	}
}

// TestAPanicInSendRecordsTheTrace is T073 and FR-071.
//
// The assertion names a frame from this goroutine rather than checking that the
// field is non-empty: FR-071's value is a trace that says where the panic came
// from, and any non-empty string satisfies a presence check — including one
// manufactured from the error text, which is what the requirement forbids.
func TestAPanicInSendRecordsTheTrace(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &panickingSink{})

	trace, ok := fieldOf(t, records, "telegram_send_failed", "stack")
	if !ok {
		t.Fatal("a panicking Send produced no stack; FR-071 records traces for panics")
	}

	text, isString := trace.(string)
	if !isString {
		t.Fatalf("the stack field is %T, want a string", trace)
	}

	// The panicking function's own frame. A trace captured anywhere other than
	// inside the deferred recover — reconstructed later, or taken from the
	// recording goroutine — would not contain it.
	if !strings.Contains(text, "panickingSink") {
		t.Errorf("the stack does not name the panicking sink, so it is not the trace of this panic:\n%s", text)
	}
}

// TestAnExpectedOperationalErrorGetsNoTrace is FR-071's prohibition.
//
// An ordinary Send error is the shape FR-071 names — a timeout, a 401, a
// missing note — and it must not get an artificially manufactured trace. The
// test is the absence of the field, which is what "not manufactured" looks like
// from outside.
func TestAnExpectedOperationalErrorGetsNoTrace(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path,
		&noteSink{note: "today.md"},
		&chatSink{sendErr: errSendFailed},
	)

	if _, ok := fieldOf(t, records, "telegram_send_failed", "stack"); ok {
		t.Error("an ordinary Send failure carries a stack; FR-071 forbids manufacturing one for an expected error")
	}

	if carrying := recordsWith(records, "stack"); len(carrying) != 0 {
		t.Errorf("%d records carry a stack for a post that only had an expected failure", len(carrying))
	}
}

// TestStackTraceOffSuppressesAnAvailableTrace pins the setting.
//
// The panic still happens and the trace is still captured at the recover point
// — internal/post does not read settings — so this asserts the recording layer
// honours the setting rather than the capture being conditional.
func TestStackTraceOffSuppressesAnAvailableTrace(t *testing.T) {
	settings, path := loggedSettings(t)
	settings.Logging.StackTrace = false

	records := postThrough(t, settings, path, &panickingSink{})

	if _, ok := fieldOf(t, records, "telegram_send_failed", "stack"); ok {
		t.Error("stack_trace = false still recorded a trace")
	}

	// The failure itself is still reported: the setting governs the trace, not
	// whether a panic becomes a logged failure.
	if _, ok := fieldOf(t, records, "telegram_send_failed", "error"); !ok {
		t.Error("stack_trace = false also dropped the error; the setting governs only the trace")
	}
}

// TestAPanickingNameRecordsItsTraceOnTheTerminalRecord covers the panic that
// has no lifecycle record to ride on (issue #110).
func TestAPanickingNameRecordsItsTraceOnTheTerminalRecord(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &namelessSink{sendErr: errSendFailed})

	trace, ok := fieldOf(t, records, "request_completed_with_error", "stack")
	if !ok {
		t.Fatal("a panicking Name left no trace anywhere; its panic has no other record to carry one")
	}

	if text, _ := trace.(string); !strings.Contains(text, "namelessSink") {
		t.Errorf("the terminal record's stack does not name the panicking sink:\n%s", text)
	}
}

// TestACapturedBodyContainingTheBotTokenIsRedacted is FR-069 against the field
// this batch adds.
//
// The batch-10 preparation named this as a question to settle before
// implementing: the capture rule puts arbitrary user text into a record, and if
// that text happens to contain the bot token then FR-068 and FR-069 point in
// opposite directions. They do not conflict, because Options.Redact scrubs at
// the one place every record passes through rather than at each call site — so
// the answer is that the chokepoint already covers a field that did not exist
// when it was written. That is worth a test precisely because it is a property
// of code this batch did not touch.
func TestACapturedBodyContainingTheBotTokenIsRedacted(t *testing.T) {
	settings, path := loggedSettings(t)

	token := settings.Sink.Telegram.BotToken.Reveal()
	if token == "" {
		t.Fatal("the fixture has no bot token, so this test would pass vacuously")
	}

	logger := app.OpenLogger(settings, logging.SourceCLI)

	post.New([]post.Sink{&chatSink{sendErr: errSendFailed}}, app.SinkTimeout(settings),
		app.NewRecording(logger, settings.Logging)).
		Post(post.Message{Original: "my token is " + token + " do not leak it"})

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw := readLog(t, path)

	if strings.Contains(raw, token) {
		t.Errorf("the bot token reached the log through the captured message body:\n%s", raw)
	}

	// Not a vacuous pass: the body must still be there, scrubbed, or this test
	// would also pass if the capture had simply been dropped.
	if !strings.Contains(raw, "do not leak it") {
		t.Errorf("the body was not captured at all, so the redaction above proves nothing:\n%s", raw)
	}
}

// TestClaimBodyWithNoBodyEmitsNothing covers the guard that cannot be reached
// through a sink.
//
// post.Message.Validate refuses an empty message, so no real post arrives at a
// failure record with no body — the branch exists for the wiring bug where
// MessageReceived was never called. Without this the guard would be a branch
// nobody has ever executed, sitting in the function that decides whether the
// user's private text reaches the log.
func TestClaimBodyWithNoBodyEmitsNothing(t *testing.T) {
	probe := app.NewCaptureProbe(true)

	body, emitted := probe.ClaimBody()

	if body != "" {
		t.Errorf("claimBody invented %q for a recorder that never saw a message", body)
	}

	// The claim must not be spent either, or the terminal record's fallback
	// would be disabled by a body that was never emitted.
	if emitted {
		t.Error("claimBody marked the body emitted although it emitted nothing")
	}
}

// TestClaimBodyReturnsNothingWhenCaptureIsUnconditional pins the other early
// return: with message_on_error_only off, MessageReceived has already emitted
// the body and a failure record must not repeat it.
func TestClaimBodyReturnsNothingWhenCaptureIsUnconditional(t *testing.T) {
	probe := app.NewCaptureProbe(false)
	probe.SetBody("already on the intake record")

	if body, _ := probe.ClaimBody(); body != "" {
		t.Errorf("a failure record would repeat the body as %q", body)
	}
}

// TestKeepTraceKeepsTheFirstTrace covers both of keepTrace's guards.
//
// The empty case is what a non-panicking NameErr or a disabled stack_trace
// produces, and the second-trace case is two sinks panicking in Name in one
// post. Neither is reachable as a distinguishable assertion through a sink: the
// terminal record carries one stack field either way, so a keepTrace that kept
// the *last* trace would look identical from outside.
func TestKeepTraceKeepsTheFirstTrace(t *testing.T) {
	probe := app.NewCaptureProbe(true)

	if got := probe.KeepTrace(""); got != "" {
		t.Errorf("an empty trace was stored as %q", got)
	}

	if got := probe.KeepTrace("first goroutine dump"); got != "first goroutine dump" {
		t.Errorf("the first trace was stored as %q", got)
	}

	if got := probe.KeepTrace("second goroutine dump"); got != "first goroutine dump" {
		t.Errorf("a second panic overwrote the first trace with %q", got)
	}

	// And an empty offer after a real one does not clear it.
	if got := probe.KeepTrace(""); got != "first goroutine dump" {
		t.Errorf("an empty trace cleared the stored one, leaving %q", got)
	}
}

// errFormattingRejected stands in for Telegram's rejection of markdown, which
// is what triggers FR-039's plaintext rescue.
var errFormattingRejected = errors.New("can't parse entities")

// rescuedSink models the telegram sink's FR-039 rescue: it reports a failed
// markdown attempt, then a plaintext attempt, and its overall result is the
// plaintext one.
//
// A stand-in rather than the real sink with an HTTP server, because what is
// under test is which records carry the body, and the rescue's two
// ReportFormatting calls are the whole mechanism that matters here.
type rescuedSink struct{ plaintextErr error }

func (s *rescuedSink) Name() string { return telegram.SinkName }

func (s *rescuedSink) Send(ctx context.Context, _ post.Message) error {
	post.ReportFormatting(ctx, post.FormattingAttempt{
		Err: errFormattingRejected, Duration: time.Millisecond,
	})
	post.ReportFormatting(ctx, post.FormattingAttempt{
		Plain: true, Err: s.plaintextErr, Duration: time.Millisecond,
	})

	return s.plaintextErr
}

// TestARescuedPostIsASuccessAndRecordsNoBody is FR-068's privacy half against
// FR-039's rescue path.
//
// The rescue is not an exotic path: it exists because Telegram rejects ordinary
// punctuation, so a markdown attempt failing and a plaintext attempt succeeding
// is a routine, fully successful post. FR-068 and log-events.md both say a post
// that fully succeeded carries the body on no record — and
// telegram_markdown_failed is a failure-shaped record inside a successful post,
// which is the one place "attach the body to failure records" and "a successful
// post records no body" disagree.
func TestARescuedPostIsASuccessAndRecordsNoBody(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &rescuedSink{})

	// Precondition: the post really did succeed, or this test proves nothing.
	if _, ok := fieldOf(t, records, "telegram_plaintext_succeeded", "sink"); !ok {
		t.Fatal("the rescue did not succeed, so this is not the success path")
	}

	if _, ok := fieldOf(t, records, "request_completed", "duration_ms"); !ok {
		t.Fatal("the post did not complete successfully, so this is not the success path")
	}

	if carrying := recordsWith(records, "message"); len(carrying) != 0 {
		t.Errorf("a rescued — and therefore successful — post put the body on %d record(s); "+
			"FR-068 requires none", len(carrying))

		for _, record := range carrying {
			t.Errorf("  event %v carried the body", record["event"])
		}
	}
}

// TestAFailedRescueRecordsTheBody is the other side: when the plaintext attempt
// also fails the post failed, and SC-008 wants the body.
func TestAFailedRescueRecordsTheBody(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &rescuedSink{plaintextErr: errSendFailed})

	if carrying := recordsWith(records, "message"); len(carrying) == 0 {
		t.Error("a post whose rescue also failed recorded no body; SC-008 cannot be met")
	}

	for _, record := range recordsWith(records, "message") {
		if record["message"] != "記録される投稿" {
			t.Errorf("event %v captured %q, want the original message", record["event"], record["message"])
		}
	}
}

// TestAPanickingNameOnASuccessfulPostStillRecordsItsTrace is FR-071 against the
// post's outcome.
//
// The two requirements are scoped differently and this is where they part. A
// sink whose Name panics but whose Send succeeds produces a delivered post — so
// FR-068 says no body — while FR-071's trace is governed by availability, and
// the frames were captured. The terminal record is the only record that could
// carry it, because a panicking Name resolves to a sentinel with no lifecycle
// events. An earlier version dropped the trace here, leaving the one failure
// class FR-071 exists for recorded without it.
func TestAPanickingNameOnASuccessfulPostStillRecordsItsTrace(t *testing.T) {
	settings, path := loggedSettings(t)

	records := postThrough(t, settings, path, &namelessSink{})

	trace, ok := fieldOf(t, records, "request_completed", "stack")
	if !ok {
		t.Fatal("a recovered Name panic on a successful post left no trace anywhere")
	}

	if text, _ := trace.(string); !strings.Contains(text, "namelessSink") {
		t.Errorf("the trace does not name the panicking sink:\n%s", text)
	}

	// And still no body: the post succeeded.
	if carrying := recordsWith(records, "message"); len(carrying) != 0 {
		t.Errorf("a successful post carried the body on %d record(s) alongside the trace", len(carrying))
	}
}
