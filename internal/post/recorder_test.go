package post

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordedEvent is one call the orchestrator made on a Recorder, flattened to
// the parts a sequence assertion cares about.
//
// A recorded sequence rather than a set of counters, because the whole of this
// package's half of T040 is *which* calls happen and *in what order*: a start
// after the finish it belongs to, or two finishes for one abandoned sink, are
// both records that parse and mislead. A counting spy cannot see either.
type recordedEvent struct {
	kind    string
	sink    string
	target  string
	nameErr error
	success bool
	err     error
}

const (
	eventReceived  = "received"
	eventStarted   = "started"
	eventFinished  = "finished"
	eventCompleted = "completed"
)

// spyRecording is a Recording whose recorder appends every call.
type spyRecording struct {
	// panicOnPost makes Recording.Post itself panic, which is the one place a
	// diagnostics failure could turn a submission into a non-submission.
	panicOnPost bool

	// recorder is created on the first Post so a test can read it afterwards.
	recorder *spyRecorder
}

func (s *spyRecording) Post(id string) Recorder {
	if s.panicOnPost {
		panic("the recording refused to start")
	}

	s.recorder = &spyRecorder{id: id}

	return s.recorder
}

// spyRecorder collects the calls for one post.
type spyRecorder struct {
	id string

	// panicOn names a call kind this recorder panics on, for the guard tests.
	panicOn string

	mu      sync.Mutex
	events  []recordedEvent
	elapsed time.Duration
}

func (s *spyRecorder) add(event recordedEvent) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()

	if s.panicOn == event.kind {
		panic("the recorder panicked on " + event.kind)
	}
}

func (s *spyRecorder) MessageReceived(Message) {
	s.add(recordedEvent{kind: eventReceived})
}

func (s *spyRecorder) SinkStarted(attempt SinkAttempt) {
	s.add(recordedEvent{
		kind:    eventStarted,
		sink:    attempt.Sink,
		target:  attempt.Target,
		nameErr: attempt.NameErr,
	})
}

func (s *spyRecorder) SinkFinished(attempt SinkAttempt, result SinkResult) {
	s.add(recordedEvent{
		kind:    eventFinished,
		sink:    attempt.Sink,
		target:  attempt.Target,
		nameErr: attempt.NameErr,
		success: result.Success,
		err:     result.Err,
	})
}

func (s *spyRecorder) PostCompleted(outcome Outcome, elapsed time.Duration) {
	s.mu.Lock()
	s.elapsed = elapsed
	s.mu.Unlock()

	s.add(recordedEvent{kind: eventCompleted, success: outcome.Succeeded()})
}

func (s *spyRecorder) recorded() []recordedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]recordedEvent(nil), s.events...)
}

// kinds returns the recorded call kinds in order.
func (s *spyRecorder) kinds() []string {
	events := s.recorded()
	kinds := make([]string, 0, len(events))

	for _, event := range events {
		kinds = append(kinds, event.kind)
	}

	return kinds
}

// forSink returns the calls recorded for one sink, in order.
func (s *spyRecorder) forSink(name string) []recordedEvent {
	var matching []recordedEvent

	for _, event := range s.recorded() {
		if event.sink == name {
			matching = append(matching, event)
		}
	}

	return matching
}

// indexOf returns the position of the first call of a kind, or -1.
func indexOf(kinds []string, kind string) int {
	for i, k := range kinds {
		if k == kind {
			return i
		}
	}

	return -1
}

// reportingSink resolves a destination and reports it, as the obsidian sink
// does.
type reportingSink struct {
	name   string
	target string

	// reports is how many times Send calls ReportTarget, so the
	// only-the-first-counts rule is assertable.
	reports int

	// before runs inside Send before the destination is reported, so a test
	// can park in the window where the sink has resolved nothing yet.
	before func()

	sendErr error
}

func (s *reportingSink) Name() string { return s.name }

func (s *reportingSink) ReportsTarget() {}

func (s *reportingSink) Send(ctx context.Context, _ Message) error {
	// Runs before the report when set, which is the shape that leaves the
	// start event with nothing to wait for.
	if s.before != nil {
		s.before()
	}

	reports := s.reports
	if reports == 0 {
		reports = 1
	}

	for i := range reports {
		ReportTarget(ctx, fmt.Sprintf("%s%s", s.target, strings.Repeat("-again", i)))
	}

	return s.sendErr
}

// silentReportingSink declares that it reports and then does not, which is the
// shape whose start event has nothing to wait for.
type silentReportingSink struct{ name string }

func (s *silentReportingSink) Name() string   { return s.name }
func (s *silentReportingSink) ReportsTarget() {}
func (s *silentReportingSink) Send(context.Context, Message) error {
	return nil
}

// TestThePostRecordsItsWholeLifecycle covers the sequence FR-067 requires: the
// post's arrival, each sink's start and outcome, then the post's terminal
// record.
func TestThePostRecordsItsWholeLifecycle(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	outcome := New([]Sink{succeeds("obsidian"), succeeds("telegram")}, generousTimeout, spy).
		Post(mustMessage(t, "両方に投げる"))

	if !outcome.Succeeded() {
		t.Fatalf("the post failed: %v", outcome.Results)
	}

	recorder := spy.recorder
	if recorder == nil {
		t.Fatal("Recording.Post was never called, so no record carries the post's identifier")
	}

	if recorder.id != outcome.ID.String() {
		t.Errorf("the recorder was bound to %q, want the post's own identifier %q",
			recorder.id, outcome.ID.String())
	}

	kinds := recorder.kinds()

	// Six calls: one arrival, two starts, two finishes, one completion. An
	// extra or a missing one is the defect, so the count is asserted rather
	// than just the endpoints.
	if len(kinds) != 6 {
		t.Fatalf("recorded %d calls (%v), want 6", len(kinds), kinds)
	}

	if kinds[0] != eventReceived {
		t.Errorf("the first call was %q, want the post's arrival before any sink ran", kinds[0])
	}

	if last := kinds[len(kinds)-1]; last != eventCompleted {
		t.Errorf("the last call was %q, want the post's terminal record", last)
	}

	for _, name := range []string{"obsidian", "telegram"} {
		events := recorder.forSink(name)

		if len(events) != 2 {
			t.Fatalf("sink %s recorded %d calls, want a start and a finish", name, len(events))
		}

		if events[0].kind != eventStarted || events[1].kind != eventFinished {
			t.Errorf("sink %s recorded %q then %q, want a start then a finish",
				name, events[0].kind, events[1].kind)
		}

		if !events[1].success {
			t.Errorf("sink %s was recorded as failed, want the success its result reports", name)
		}
	}

	if !recorder.recorded()[len(kinds)-1].success {
		t.Error("the terminal record says the post failed; it must track the exit status")
	}

	if recorder.elapsed <= 0 {
		t.Errorf("the post's elapsed time was recorded as %v, want a positive duration", recorder.elapsed)
	}
}

// TestAReportingSinksStartCarriesTheNoteItResolved is issue #98's start-event
// half, and the reason DEC-D3 puts the report on the context.
//
// The start event is emitted *at* the report rather than before Send, so the
// path is the one the sink resolved for this post. Emitting it before Send would
// pass a test that only checks the succeeded record.
func TestAReportingSinksStartCarriesTheNoteItResolved(t *testing.T) {
	t.Parallel()

	const note = "/vault/2026-09-10.md"

	spy := &spyRecording{}
	sink := &reportingSink{name: "obsidian", target: note}

	if _, declares := any(sink).(TargetReporting); !declares {
		t.Fatal("the fixture does not declare TargetReporting, so this test would not exercise the hold")
	}

	New([]Sink{sink}, generousTimeout, spy).Post(mustMessage(t, "場所付きで記録する"))

	events := spy.recorder.forSink("obsidian")
	if len(events) != 2 {
		t.Fatalf("recorded %d calls for the sink, want a start and a finish", len(events))
	}

	for _, event := range events {
		if event.target != note {
			t.Errorf("the %s record carries target %q, want the note the sink resolved (%q)",
				event.kind, event.target, note)
		}
	}
}

// TestANonReportingSinksStartIsRecordedBeforeItsSend covers the other arm of
// the same decision.
//
// A sink that reports nothing must not have its start held back: the record
// would then be written after the send it describes had finished, and a send
// interrupted by a killed process would leave no trace of having begun.
func TestANonReportingSinksStartIsRecordedBeforeItsSend(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	var startedBeforeSend bool

	sink := &fakeSink{name: "telegram", send: func(context.Context, Message) error {
		// Read from inside Send, which is the only place the ordering is
		// visible: after Send returns both orders look identical.
		startedBeforeSend = len(spy.recorder.forSink("telegram")) == 1

		return nil
	}}

	New([]Sink{sink}, generousTimeout, spy).Post(mustMessage(t, "パスは無い"))

	if !startedBeforeSend {
		t.Error("the sink's start was not recorded before Send was entered")
	}

	events := spy.recorder.forSink("telegram")
	if len(events) != 2 {
		t.Fatalf("recorded %d calls, want a start and a finish", len(events))
	}

	for _, event := range events {
		if event.target != "" {
			t.Errorf("the %s record carries target %q; a sink that reports none must have no path",
				event.kind, event.target)
		}
	}
}

// TestASilentReportingSinkStillGetsAStart covers the fallback for a sink that
// declares it reports and then does not.
//
// Without it the start event would simply never be emitted, and the log would
// carry a succeeded record for a send it never saw begin.
func TestASilentReportingSinkStillGetsAStart(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	New([]Sink{&silentReportingSink{name: "obsidian"}}, generousTimeout, spy).
		Post(mustMessage(t, "報告しない"))

	events := spy.recorder.forSink("obsidian")
	if len(events) != 2 {
		t.Fatalf("recorded %d calls, want a start and a finish", len(events))
	}

	if events[0].kind != eventStarted {
		t.Errorf("the first call was %q, want a start emitted for a sink that reported nothing",
			events[0].kind)
	}

	if events[0].target != "" {
		t.Errorf("the start carries target %q, want none", events[0].target)
	}
}

// TestOnlyTheFirstReportCounts pins ReportTarget's documented rule.
//
// The start record is already written by the time a second report arrives, so
// honouring it would either duplicate the record or leave the finish records
// naming a different file from the start.
func TestOnlyTheFirstReportCounts(t *testing.T) {
	t.Parallel()

	const note = "/vault/first.md"

	spy := &spyRecording{}

	New([]Sink{&reportingSink{name: "obsidian", target: note, reports: 3}}, generousTimeout, spy).
		Post(mustMessage(t, "二度目は無視される"))

	events := spy.recorder.forSink("obsidian")
	if len(events) != 2 {
		t.Fatalf("recorded %d calls for three reports, want a start and a finish", len(events))
	}

	for _, event := range events {
		if event.target != note {
			t.Errorf("the %s record carries %q, want the first reported note %q",
				event.kind, event.target, note)
		}
	}
}

// TestAnAbandonedSinkIsRecordedOnceAndInOrder covers the sink the backstop gave
// up on.
//
// The sink declares that it reports a destination and then blocks *before*
// reporting one, so its start event has nothing to wait for and has to come from
// the backstop instead. FR-070 wants every failure recorded, and this failure is
// one the orchestrator caused: leaving it to the abandoned goroutine would mean
// the record arrived after the post's own terminal record, or on a hard-mounted
// dead export never at all.
//
// The sink is then released and its Send allowed to return, and the records are
// asserted again — so a later change that had the abandoned goroutine report its
// own outcome would show up here as a second failure record for one sink rather
// than passing quietly.
func TestAnAbandonedSinkIsRecordedOnceAndInOrder(t *testing.T) {
	t.Parallel()

	var (
		release  = make(chan struct{})
		returned = make(chan struct{})
	)

	spy := &spyRecording{}

	blocked := &reportingSink{
		name:   "obsidian",
		target: "/vault/never-reported.md",
		before: func() {
			<-release
			close(returned)
		},
	}

	outcome := New([]Sink{blocked}, 50*time.Millisecond, spy).
		Post(mustMessage(t, "見捨てられる"))

	if outcome.Succeeded() {
		t.Fatal("the abandoned sink was reported as a success")
	}

	assertAbandonedRecords := func(t *testing.T, when string) {
		t.Helper()

		events := spy.recorder.forSink("obsidian")

		if len(events) != 2 {
			t.Fatalf("%s: recorded %d calls for one abandoned sink (%v), want exactly a start "+
				"and a finish", when, len(events), spy.recorder.kinds())
		}

		if events[0].kind != eventStarted || events[1].kind != eventFinished {
			t.Errorf("%s: recorded %q then %q, want a start then a finish",
				when, events[0].kind, events[1].kind)
		}

		if events[0].target != "" {
			t.Errorf("%s: the start carries target %q; the sink never reported one",
				when, events[0].target)
		}

		if events[1].success {
			t.Errorf("%s: the abandoned sink's finish records a success", when)
		}

		if !errors.Is(events[1].err, context.DeadlineExceeded) {
			t.Errorf("%s: the recorded error is %v, want it to wrap context.DeadlineExceeded",
				when, events[1].err)
		}
	}

	assertAbandonedRecords(t, "while the sink is still blocked")

	// Let the abandoned goroutine run to completion and report anything it
	// wants to. Waiting on the sink's own signal rather than on the record
	// count, so the second assertion is taken after the goroutine has had its
	// chance rather than before it.
	close(release)
	<-returned

	// Its Send returns immediately after the signal, and the deferred send in
	// run's goroutine is next. Give the scheduler room for that to land before
	// asserting nothing arrived.
	for range 100 {
		if len(spy.recorder.forSink("obsidian")) > 2 {
			break
		}

		time.Sleep(time.Millisecond)
	}

	assertAbandonedRecords(t, "after the abandoned sink returned")
}

// TestTheTerminalRecordFollowsTheWholePost covers the pairing between the
// terminal record and the exit status (FR-059, FR-060).
func TestTheTerminalRecordFollowsTheWholePost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		sinks []Sink
		want  bool
	}{
		{name: "both succeeded", sinks: []Sink{succeeds("obsidian"), succeeds("telegram")}, want: true},
		{
			name:  "one failed",
			sinks: []Sink{succeeds("obsidian"), fails("telegram", errors.New("chat not found"))},
			want:  false,
		},
		{
			name:  "both failed",
			sinks: []Sink{fails("obsidian", errors.New("read-only")), fails("telegram", errors.New("401"))},
			want:  false,
		},
		{name: "no sink at all", sinks: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			spy := &spyRecording{}

			outcome := New(test.sinks, generousTimeout, spy).Post(mustMessage(t, "結果は一つ"))

			events := spy.recorder.recorded()
			if len(events) == 0 {
				t.Fatal("nothing was recorded")
			}

			terminal := events[len(events)-1]
			if terminal.kind != eventCompleted {
				t.Fatalf("the last call was %q, want the terminal record", terminal.kind)
			}

			if terminal.success != test.want {
				t.Errorf("the terminal record says succeeded=%t, want %t", terminal.success, test.want)
			}

			if terminal.success != outcome.Succeeded() {
				t.Errorf("the terminal record says succeeded=%t while the outcome says %t",
					terminal.success, outcome.Succeeded())
			}
		})
	}
}

// TestAPostWithNoSinkIsStillRecorded covers the early return.
//
// FR-018 makes an all-disabled configuration a startup error, so this is
// unreachable in a wired application — but a post that reached the orchestrator
// and delivered nothing is exactly the run an operator would be trying to
// explain, and the early return used to leave no trace of it at all.
func TestAPostWithNoSinkIsStillRecorded(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	New(nil, generousTimeout, spy).Post(mustMessage(t, "宛先が無い"))

	if got, want := spy.recorder.kinds(), []string{eventReceived, eventCompleted}; !equalStrings(got, want) {
		t.Errorf("recorded %v, want %v", got, want)
	}
}

// TestANamePanicReachesTheRecorder is issue #110's acceptance.
//
// A sink whose Name panics while Send succeeds used to produce a clean success
// result with the panic value destroyed at the layer that recovered it, so
// nothing anywhere said a panic had happened. Success must still follow what
// Send did (FR-019 rules out an automatic resend, so a sink that delivered must
// not be reported as failed) — the defect was only the missing evidence.
func TestANamePanicReachesTheRecorder(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	sink := &nameOnlySink{value: "Name exploded", sendErr: nil}

	outcome := New([]Sink{sink}, generousTimeout, spy).Post(mustMessage(t, "名前で落ちる"))

	events := spy.recorder.forSink(unknownSinkName)
	if len(events) != 2 {
		t.Fatalf("recorded %d calls for the unattributable sink (%v), want a start and a finish",
			len(events), spy.recorder.recorded())
	}

	if events[0].nameErr == nil {
		t.Fatal("the recovered Name panic did not reach the recorder; " +
			"a sink that panics in Name on every post would be invisible")
	}

	if got := events[0].nameErr.Error(); !strings.Contains(got, "Name exploded") {
		t.Errorf("the diagnostic reads %q, want it to carry the panic value", got)
	}

	// The result is still what Send did.
	if len(outcome.Results) != 1 {
		t.Fatalf("got %d results, want one", len(outcome.Results))
	}

	if !outcome.Results[0].Success {
		t.Error("a sink whose Send succeeded was reported as failed because its Name panicked")
	}

	if outcome.Results[0].Err != nil {
		t.Errorf("the successful result carries Err = %v, want nil", outcome.Results[0].Err)
	}
}

// TestANilRecordingChangesNothing covers FR-076 from the construction side.
//
// A front door whose logger could not be opened still posts, and the outcome
// must be identical. Every one of this package's other tests passes nil, so the
// claim is exercised throughout; this asserts it rather than relying on that.
func TestANilRecordingChangesNothing(t *testing.T) {
	t.Parallel()

	sinks := []Sink{succeeds("obsidian"), fails("telegram", errors.New("chat not found"))}

	outcome := New(sinks, generousTimeout, nil).Post(mustMessage(t, "記録は無い"))

	if len(outcome.Results) != 2 {
		t.Fatalf("got %d results, want two", len(outcome.Results))
	}

	if outcome.Succeeded() {
		t.Error("a post with one failing sink reported success")
	}

	if !resultFor(t, outcome, "obsidian").Success {
		t.Error("the succeeding sink was not reported as a success")
	}
}

// TestARecordingCannotTakeDownAPost covers the guard.
//
// A Recorder runs on the delivery goroutines, so an unrecovered panic there
// unwinds one: the sink never reports, its sibling's result is never collected,
// and the exit status describes a post that did not finish. FR-076 is explicit
// that a diagnostics failure changes nothing about the post.
func TestARecordingCannotTakeDownAPost(t *testing.T) {
	t.Parallel()

	panicOn := []string{eventReceived, eventStarted, eventFinished, eventCompleted}

	for _, kind := range panicOn {
		t.Run("panics on "+kind, func(t *testing.T) {
			t.Parallel()

			recording := &hostileRecording{panicOn: kind}

			outcome := New([]Sink{succeeds("obsidian"), succeeds("telegram")}, generousTimeout, recording).
				Post(mustMessage(t, "記録が壊れている"))

			if len(outcome.Results) != 2 {
				t.Fatalf("got %d results, want two", len(outcome.Results))
			}

			if !outcome.Succeeded() {
				t.Errorf("the post failed because the recorder panicked: %v", outcome.Results)
			}
		})
	}

	t.Run("panics when the post's recorder is built", func(t *testing.T) {
		t.Parallel()

		outcome := New([]Sink{succeeds("obsidian")}, generousTimeout, &spyRecording{panicOnPost: true}).
			Post(mustMessage(t, "記録を始められない"))

		if !outcome.Succeeded() {
			t.Errorf("the post failed because Recording.Post panicked: %v", outcome.Results)
		}
	})

	t.Run("returns a nil recorder", func(t *testing.T) {
		t.Parallel()

		outcome := New([]Sink{succeeds("obsidian")}, generousTimeout, nilRecording{}).
			Post(mustMessage(t, "記録しない実装"))

		if !outcome.Succeeded() {
			t.Errorf("the post failed for a Recording that records nothing: %v", outcome.Results)
		}
	})

	t.Run("returns a typed nil recorder", func(t *testing.T) {
		t.Parallel()

		outcome := New([]Sink{succeeds("obsidian")}, generousTimeout, typedNilRecording{}).
			Post(mustMessage(t, "型付きの nil"))

		if !outcome.Succeeded() {
			t.Errorf("the post failed for a Recording returning a typed nil: %v", outcome.Results)
		}
	})
}

// hostileRecording hands out a recorder that panics on one kind of call.
type hostileRecording struct{ panicOn string }

func (h *hostileRecording) Post(string) Recorder {
	return &spyRecorder{panicOn: h.panicOn}
}

// nilRecording records nothing, which Recording.Post is documented to allow.
type nilRecording struct{}

func (nilRecording) Post(string) Recorder { return nil }

// typedNilRecording returns a nil pointer inside a non-nil interface, which the
// nil check cannot see and the panic guard has to contain.
type typedNilRecording struct{}

func (typedNilRecording) Post(string) Recorder { return (*spyRecorder)(nil) }

// TestReportTargetWithoutAReporterIsHarmless covers a sink run outside a post,
// which is what its own package's tests do.
func TestReportTargetWithoutAReporterIsHarmless(t *testing.T) {
	t.Parallel()

	ReportTarget(context.Background(), "/vault/2026-09-10.md")

	// A reporter installed as a nil function is the other shape, and the one a
	// caller could reach by accident.
	ReportTarget(WithTargetReporter(context.Background(), nil), "/vault/2026-09-10.md")
}

// TestErrorTypeAndReasonAgree pins the property that keeps the display reason
// and the classified type from drifting.
//
// They are two renderings of one judgement. A classifier that answered
// "timeout" for a result the front door calls "delivery failed" would make the
// log and the terminal describe the same failure differently, and nothing else
// in the repository would notice.
func TestErrorTypeAndReasonAgree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantReason string
		wantType   string
	}{
		{
			name:       "the sink's own deadline",
			err:        context.DeadlineExceeded,
			wantReason: reasonTimedOut,
			wantType:   errorTypeTimeout,
		},
		{
			name:       "a wrapped deadline",
			err:        fmt.Errorf("appending to /vault/note.md: %w", context.DeadlineExceeded),
			wantReason: reasonTimedOut,
			wantType:   errorTypeTimeout,
		},
		{
			name:       "an ordinary failure",
			err:        errors.New("chat not found"),
			wantReason: reasonFailed,
			wantType:   errorTypeFailed,
		},
		{
			name:       "a cancellation, which is not a deadline",
			err:        context.Canceled,
			wantReason: reasonFailed,
			wantType:   errorTypeFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := reasonFor(test.err); got != test.wantReason {
				t.Errorf("reasonFor = %q, want %q", got, test.wantReason)
			}

			result := SinkResult{Name: "telegram", Reason: test.wantReason, Err: test.err}

			if got := ErrorType(result); got != test.wantType {
				t.Errorf("ErrorType = %q, want %q", got, test.wantType)
			}
		})
	}
}

// TestErrorTypeIsEmptyForASuccess keeps the field off the records that must not
// carry it: contracts/log-events.md puts error_type on failures.
func TestErrorTypeIsEmptyForASuccess(t *testing.T) {
	t.Parallel()

	if got := ErrorType(SinkResult{Name: "obsidian", Success: true}); got != "" {
		t.Errorf("ErrorType for a success = %q, want empty", got)
	}
}

// TestErrorTypeClassifiesAFailureWithNoError covers the shape no sink produces
// and the contract still requires a value for.
func TestErrorTypeIsSpecificEvenWithNoError(t *testing.T) {
	t.Parallel()

	if got := ErrorType(SinkResult{Name: "obsidian", Reason: reasonFailed}); got != errorTypeFailed {
		t.Errorf("ErrorType for a failure with no error = %q, want %q", got, errorTypeFailed)
	}
}

// TestEveryRecordedSinkNameMatchesItsResult keeps the identity fields
// consistent across a sink's records.
//
// The name reaches the recorder by a different route from the one it reaches
// SinkResult by — the record is told, the result is built — so a divergence
// would produce records that cannot be correlated with the report the user saw.
func TestEveryRecordedSinkNameMatchesItsResult(t *testing.T) {
	t.Parallel()

	spy := &spyRecording{}

	sinks := []Sink{
		succeeds("obsidian"),
		fails("telegram", errors.New("chat not found")),
	}

	outcome := New(sinks, generousTimeout, spy).Post(mustMessage(t, "名前は一致する"))

	for _, result := range outcome.Results {
		events := spy.recorder.forSink(result.Name)
		if len(events) != 2 {
			t.Errorf("sink %s has %d records, want two", result.Name, len(events))
		}
	}
}

// equalStrings compares two string slices element by element.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

// TestASinkRecordEmitsEachStageOnce pins the arbiter's contract directly.
//
// The orchestrator calls finish once per sink — run returns after one arm of its
// select — so a post cannot exercise the second-call guard, and a guard nothing
// can fail is worth no more than its comment. This calls the type the way a
// later change might: an abandoned goroutine reporting its own late outcome
// after the backstop already reported a timeout, and a sink reporting a
// destination after its record was written.
func TestASinkRecordEmitsEachStageOnce(t *testing.T) {
	t.Parallel()

	spy := &spyRecorder{}
	record := newSinkRecord(spy)

	record.named("obsidian", nil)

	record.start("/vault/first.md")
	record.start("/vault/second.md")
	record.start("")

	timedOut := SinkResult{Name: "obsidian", Reason: reasonTimedOut, Err: context.DeadlineExceeded}
	late := SinkResult{Name: "obsidian", Success: true}

	record.finish(timedOut)
	record.finish(late)

	events := spy.recorded()

	if len(events) != 2 {
		t.Fatalf("recorded %d calls for three starts and two finishes (%v), want one of each",
			len(events), spy.kinds())
	}

	if events[0].kind != eventStarted || events[1].kind != eventFinished {
		t.Fatalf("recorded %v, want a start then a finish", spy.kinds())
	}

	if events[0].target != "/vault/first.md" {
		t.Errorf("the start carries %q, want the first reported destination", events[0].target)
	}

	if events[1].success {
		t.Error("the finish records the late success; the first outcome reported is the one that stands")
	}

	if !errors.Is(events[1].err, context.DeadlineExceeded) {
		t.Errorf("the finish carries %v, want the timeout that was reported first", events[1].err)
	}
}

// TestASinkRecordOrdersAStartBeforeAFinishItNeverSaw covers the fallback in
// isolation: a sink abandoned before it reported anything still has a
// lifecycle, because FR-070 wants every failure recorded and a failure the log
// never saw begin is harder to read than one it did.
func TestASinkRecordOrdersAStartBeforeAFinishItNeverSaw(t *testing.T) {
	t.Parallel()

	spy := &spyRecorder{}
	record := newSinkRecord(spy)

	record.finish(SinkResult{Name: unknownSinkName, Reason: reasonTimedOut})

	if got := spy.kinds(); !equalStrings(got, []string{eventStarted, eventFinished}) {
		t.Fatalf("recorded %v, want a start then a finish", got)
	}

	if got := spy.recorded()[0].sink; got != unknownSinkName {
		t.Errorf("the start names %q, want the sentinel a sink whose name is unknown gets", got)
	}
}

// TestGuardHandlesNoRecorderWithoutArrestingAPanic pins the nil arm of guard.
//
// Removing it changes no outcome, because guarded's own recover absorbs the nil
// method call — two guards in series, where neither one's test can tell which
// is doing the work. That is the shape this repository has been caught by
// before, so the distinction is asserted rather than left to a comment.
//
// It is a real distinction and not tidiness. "Record nothing" is a documented,
// expected return from Recording.Post; reaching it by raising and arresting a
// panic on every one of a post's six recorder calls turns an ordinary state
// into six panics in a profile, a trace, or a debugger stopped on panics — and
// it spends the containment guard on a case that needs no containment.
func TestGuardHandlesNoRecorderWithoutArrestingAPanic(t *testing.T) {
	t.Parallel()

	if _, ok := guard(nil).(noRecorder); !ok {
		t.Errorf("guard(nil) returned %T, want the discarding recorder rather than a wrapper "+
			"that reaches this state by arresting a panic", guard(nil))
	}

	// A real recorder is still wrapped, so the containment is not lost.
	if _, ok := guard(&spyRecorder{}).(guarded); !ok {
		t.Errorf("guard returned %T for a real recorder, want it wrapped", guard(&spyRecorder{}))
	}

	// And the discarding recorder is safe to call, which is the whole of what
	// the posting path needs from it.
	discarding := guard(nil)
	discarding.MessageReceived(Message{Original: "何も記録しない"})
	discarding.SinkStarted(SinkAttempt{Sink: "obsidian"})
	discarding.SinkFinished(SinkAttempt{Sink: "obsidian"}, SinkResult{Success: true})
	discarding.PostCompleted(Outcome{}, time.Millisecond)
}
