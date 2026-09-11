package post

import (
	"sync"
	"time"
)

// Recording is what a front door hands the orchestrator so one post can be
// observed (T040, FR-067).
//
// It mirrors logging.Logger's own shape — a long-lived thing that yields a
// per-post recorder — for a reason that is not symmetry: contracts/log-events.md
// marks message_id present on every record, and requiring the identifier to
// obtain a Recorder makes omitting it impossible to express. The identifier is
// the ULID this package generates, because the orchestrator owns the post's
// identity and a logger minting its own would give one post two identifiers
// across two front doors.
//
// A domain-shaped interface declared here rather than a *logging.PostLogger
// taken as a parameter, which is decision DEC-D2. internal/post imports no
// other internal package, and that invariant is load bearing rather than
// decorative: Service takes a time.Duration instead of a config.Settings
// precisely so the settings tree does not sit behind the posting core, and
// internal/logging imports internal/config — so post -> logging would
// reintroduce post -> config transitively, by the back door service.go closes
// at the front. plan.md's Structure Decision makes package boundaries the way
// constitution principle II is enforced by the compiler rather than by review,
// and internal/app/layering_test.go fails the build if this package acquires an
// internal import.
//
// The cost is one indirection whose fidelity has to be bought back somewhere:
// the mapping onto logging.Event lives in internal/app, and every trap on issue
// #41's list — duration_ms in nanoseconds, a dropped reserved key, Redact never
// passed — is a record-shape bug rather than an orchestrator bug. So the adapter
// is tested against a real logging.Logger writing real bytes, and these tests
// assert the sequence and the payload this package is responsible for.
type Recording interface {
	// Post returns the recorder for one post, bound to its correlation
	// identifier. Returning nil is allowed and means "record nothing".
	Post(id string) Recorder
}

// Recorder receives one post's diagnostic events, in the order they happened.
//
// Four methods rather than one per event name, because the event *vocabulary*
// belongs to internal/logging and the mapping onto it belongs to internal/app.
// What this package knows is the lifecycle: a post arrived, a sink began, a sink
// finished, the post is over. Which of request_completed and
// request_completed_with_error that last one becomes, and whether a sink's
// finish is a success or a failure record, is derivable from the values passed —
// so it cannot be got wrong by calling the wrong method.
//
// Every method is called at most once per stage per sink, and a start always
// precedes the matching finish, even when a sink is abandoned mid-delivery and
// its goroutine later completes. sinkRecord is what guarantees that.
//
// An implementation must not panic, must not block, and must not assume it is
// called from any particular goroutine — sink events arrive on the delivery
// goroutines, concurrently. The orchestrator guards against the first anyway
// (see guarded): FR-076's first duty is that diagnostics never change what the
// post does.
type Recorder interface {
	// MessageReceived records the post entering the system, before any sink has
	// been started.
	MessageReceived(message Message)

	// SinkStarted records one sink beginning its delivery. For a sink that
	// reports a destination this is called at the report, so attempt.Target is
	// populated; for one that does not, it is called before Send is entered.
	SinkStarted(attempt SinkAttempt)

	// SinkFinished records one sink's outcome. result.Success selects between a
	// success and a failure record.
	SinkFinished(attempt SinkAttempt, result SinkResult)

	// PostCompleted records the post's terminal event. outcome.Succeeded
	// selects between the two names, and therefore matches the exit status
	// (FR-059, FR-060). elapsed is the whole post's wall time, which is what
	// FR-066 wants as duration_ms on a completion event; Outcome carries no
	// duration of its own, and adding one would put a field on a shipped type
	// for the sake of one caller.
	PostCompleted(outcome Outcome, elapsed time.Duration)
}

// SinkAttempt is one sink's delivery as the orchestrator knows it: the identity
// and destination half, which the SinkResult does not carry.
//
// It threads through both of that sink's recorder calls, so the sink and path
// fields on its start, success and failure records come from one value and
// cannot disagree with each other.
type SinkAttempt struct {
	// Sink is the name the sink reported, or unknownSinkName when it could not
	// be obtained. It always matches the Name on the matching SinkResult.
	Sink string

	// Target is the destination the sink reported through ReportTarget, or ""
	// when it reported none. Empty means the record carries no path field:
	// contracts/log-events.md puts path on the note events only, and the chat
	// sink reports nothing (issue #98).
	Target string

	// NameErr is the diagnostic for a sink whose Name method panicked, and nil
	// otherwise (issue #110).
	//
	// It exists because that panic used to be discarded where it was recovered,
	// so a sink that panicked in Name on every post while delivering
	// successfully produced a clean success record and left no evidence
	// anywhere. The value is gone by the time T073 could record it, so keeping
	// it needed a change at the point of recovery rather than a later task.
	//
	// A panicking Name resolves the sink's name to the sentinel, and the event
	// vocabulary is keyed by sink name, so there is no lifecycle record this
	// diagnostic can ride on: the adapter reports it on the post's terminal
	// record instead. That is also why this is a field rather than its own
	// Recorder method — there is one place it can go, whatever caused it.
	NameErr error
}

// noRecorder discards everything, and is what a Service with no Recording uses.
//
// A value of this type rather than a nil check at each of the dozen call sites.
// The call sites are on the posting path and are the code that must not acquire
// a "if the recorder exists" branch: one forgotten check is a nil dereference
// inside a sink's goroutine, which is the failure FR-076 exists to prevent,
// arriving from the diagnostics layer itself.
type noRecorder struct{}

func (noRecorder) MessageReceived(Message)              {}
func (noRecorder) SinkStarted(SinkAttempt)              {}
func (noRecorder) SinkFinished(SinkAttempt, SinkResult) {}
func (noRecorder) PostCompleted(Outcome, time.Duration) {}

// guarded stops a Recording implementation from changing what the post does.
//
// A Recorder is supplied from outside this package, so it is code this package
// cannot vet, running on the delivery goroutines. An unrecovered panic there
// unwinds the goroutine: the sink never reports, its sibling's result is never
// collected, and the exit status describes a post that did not finish — the
// coupling constitution principle I forbids, arriving through diagnostics.
// FR-076 is explicit that a diagnostics failure changes nothing about the post.
//
// The panic is swallowed here rather than reported, and that is not the whole
// story: the adapter in internal/app emits through logging.PostLogger, which
// latches a panic on its own path into the degradation the front door turns into
// FR-076's one warning. So for the implementation this program actually ships,
// the operator is still told. This guard is the backstop for the shape that
// reaches neither — a Recording that panics before it reaches the logger — where
// the choice is between losing a record and losing the post.
type guarded struct{ inner Recorder }

// guard wraps rec, mapping a nil Recorder onto the discarding one.
//
// The nil check is for an untyped nil, which is what a Recording.Post returning
// "record nothing" gives. A typed nil passes it and panics on the first method
// call, which guarded then contains — so both shapes are safe and neither is
// silently mistaken for the other.
func guard(rec Recorder) Recorder {
	if rec == nil {
		return noRecorder{}
	}

	return guarded{inner: rec}
}

// swallow arrests a panic from a recorder call. Deferred directly, because
// recover only works when called by a function the runtime deferred.
func swallow() { _ = recover() }

func (g guarded) MessageReceived(message Message) {
	defer swallow()

	g.inner.MessageReceived(message)
}

func (g guarded) SinkStarted(attempt SinkAttempt) {
	defer swallow()

	g.inner.SinkStarted(attempt)
}

func (g guarded) SinkFinished(attempt SinkAttempt, result SinkResult) {
	defer swallow()

	g.inner.SinkFinished(attempt, result)
}

func (g guarded) PostCompleted(outcome Outcome, elapsed time.Duration) {
	defer swallow()

	g.inner.PostCompleted(outcome, elapsed)
}

// recorderFor returns the recorder for one post, wrapped so nothing it does can
// affect the post.
//
// The construction is guarded too. Recording.Post is the caller's code, and a
// panic there would take down the post before any sink had run — the one place a
// diagnostics failure could turn a submission into a non-submission.
func (s *Service) recorderFor(id string) Recorder {
	if s.recording == nil {
		return noRecorder{}
	}

	return guard(recorderFrom(s.recording, id))
}

// recorderFrom calls Recording.Post without letting it panic out.
func recorderFrom(recording Recording, id string) (rec Recorder) {
	defer func() {
		if recover() != nil {
			rec = nil
		}
	}()

	return recording.Post(id)
}

// sinkRecord tracks one sink's attempt so each lifecycle stage is recorded
// exactly once, in order, whichever goroutine gets there first.
//
// Two goroutines race for this by design. The delivery goroutine reports the
// sink's destination from inside Send and finishes with the sink's own result;
// run's backstop finishes with a timeout result for a sink it abandoned, and the
// abandoned goroutine may then finish minutes later, or never. Without a single
// arbiter, an abandoned post produces two failure records for one sink, and a
// start event emitted after the finish it belongs to.
//
// The recorder is called while the mutex is held, which is deliberate rather
// than careless. The alternative — snapshot under the lock, emit outside it —
// lets a goroutine that lost the start race emit its finish before the winner
// has emitted the start, so the log's order contradicts the lifecycle. The lock
// is per sink attempt and the only other contender is that same sink's finish,
// so a slow recorder delays one sink's own records and nothing else.
type sinkRecord struct {
	rec Recorder

	mu       sync.Mutex
	attempt  SinkAttempt
	started  bool
	finished bool
}

// newSinkRecord starts tracking one attempt, named with the sentinel until the
// sink's own name is known.
//
// Seeded rather than left empty because run's backstop can finish an attempt
// before the delivery goroutine has got as far as asking the sink its name — a
// Name that blocks is exactly that case — and a record naming "" would be a
// record naming no destination at all.
func newSinkRecord(rec Recorder) *sinkRecord {
	return &sinkRecord{rec: rec, attempt: SinkAttempt{Sink: unknownSinkName}}
}

// named records what the sink called itself, and the diagnostic if asking it
// panicked (issue #110).
func (r *sinkRecord) named(name string, namePanic error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attempt.Sink = name
	r.attempt.NameErr = namePanic
}

// start emits the sink's start event, once. target is the destination the sink
// reported, or "" when it reported none.
func (r *sinkRecord) start(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.startLocked(target)
}

func (r *sinkRecord) startLocked(target string) {
	if r.started {
		return
	}

	r.started = true

	if target != "" {
		r.attempt.Target = target
	}

	r.rec.SinkStarted(r.attempt)
}

// finish emits the sink's outcome, once, after its start event.
//
// run is the only caller and calls it once per sink, from whichever arm of its
// select won — so the second-call guard is not exercised by a post today. It is
// kept rather than removed because it is what makes "one outcome record per
// sink" a property of this type instead of a property of one caller's shape,
// and the abandoned goroutine reporting its own late outcome is a change a
// later task could reasonably make. It is pinned directly by
// TestASinkRecordEmitsEachStageOnce rather than left as a guard nothing can
// fail, which is the trap issue #118 is about.
func (r *sinkRecord) finish(result SinkResult) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finished {
		return
	}

	// A sink abandoned before it reported anything still has a lifecycle, and
	// FR-070 wants every failure recorded. Emitting the start here keeps the
	// pair intact rather than logging a failure for something the log never
	// saw begin.
	r.startLocked("")

	r.finished = true

	r.rec.SinkFinished(r.attempt, result)
}
