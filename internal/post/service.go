package post

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// unknownSinkName stands in when a sink's own Name() cannot be obtained.
//
// Name() is sink code and can panic like any other, so the result for a sink
// that failed that way still has to be attributable to *something*. A sentinel
// is greppable and honest; an empty Name would give the front door a blank line
// and the log a record that names no destination.
const unknownSinkName = "unknown"

// enforcementGrace is how long past its own deadline a sink is given to notice
// cancellation before the orchestrator stops waiting for it.
//
// The per-sink context is still the primary mechanism, and a sink that honours
// it returns its own error with its own context — which is more informative
// than anything synthesised out here. Without a grace period the context
// deadline and the orchestrator's timer fire at the same instant, so which
// error a cooperative sink's result carried would be a race.
//
// The backstop only decides the outcome for a sink that cannot be cancelled at
// all, and for those the extra quarter second is irrelevant next to a stalled
// mount.
const enforcementGrace = 250 * time.Millisecond

// defaultSinkTimeout is FR-015's documented default, used when New is handed a
// duration that cannot bound anything.
const defaultSinkTimeout = 60 * time.Second

// errAbnormalExit is the diagnostic for a delivery goroutine that ended without
// returning and without panicking.
//
// runtime.Goexit does that, and so does panic(nil) when a program is built or
// run with panicnil=1. Both run deferred functions, so the recover below sees
// nothing to recover and the named result would otherwise be returned as its
// zero value: no name, no reason for the front door, no error for the log. This
// keeps such an exit a well-formed failure.
var errAbnormalExit = errors.New("the delivery goroutine ended without returning")

// Service runs one post across the sinks it was given.
//
// It holds the sinks rather than the settings, because deciding which sinks
// exist is a different job: T036 builds only the enabled ones from settings, so
// FR-016's "disabled sinks are not invoked and do not count as failures" is
// satisfied before a Service is ever constructed. This type cannot invoke a
// sink it was not handed, which is a stronger guarantee than checking a flag
// here would be.
//
// It takes a duration rather than config.Settings for the same reason and one
// more: internal/post imports no other internal package, and a dependency on
// internal/config would put the whole settings tree behind the posting core for
// the sake of one integer. The caller converts posting.sink_timeout_seconds.
type Service struct {
	sinks   []Sink
	timeout time.Duration

	// recording receives this post's diagnostic events, and is nil when the
	// caller wants none (T040, DEC-D2).
	//
	// Nil-tolerant rather than required, so a Service can be constructed for a
	// test or a front door that has no logger without either of them having to
	// supply a stub. Every call goes through recorderFor, which maps nil onto
	// the discarding recorder, so no code on the posting path branches on it.
	recording Recording

	// newID generates the per-post correlation identifier. A field rather than
	// a direct call so that identifier's failure branch is reachable from a
	// test, which sets it directly — this package's tests are in-package, so
	// no export_test.go seam is needed. Unexported, so nothing outside this
	// package can replace it.
	newID func() (ulid.ULID, error)
}

// New returns a Service that posts to sinks, allowing each one timeout for its
// entire operation (FR-015), and reporting what happens to recording (T040).
//
// The timeout is per sink and not a budget for the post as a whole: two sinks
// each get the full duration, measured independently from the moment that sink
// starts.
//
// recording may be nil, which records nothing. That is not a convenience for
// tests: a front door whose logger could not be opened must still post, and
// FR-076 requires the outcome to be identical either way — so "no diagnostics"
// has to be an ordinary state of this type rather than a degraded one.
func New(sinks []Sink, timeout time.Duration, recording Recording) *Service {
	// A duration that cannot bound anything is replaced by FR-015's default.
	//
	// config.Validate already requires posting.sink_timeout_seconds > 0, so a
	// wired application cannot get here — but the conversion from seconds is
	// the caller's, and `time.Duration(seconds) * time.Second` overflows
	// int64 silently: a settings file with 10000000000 passes validation and
	// converts to roughly -2346317h. Passed through, that would expire every
	// context before its sink was entered and report "request timed out" for a
	// post nothing ever attempted, which is a far worse diagnosis than using
	// the documented default. The upper bound belongs in config's validation;
	// this is the floor that keeps a misconfiguration from looking like a
	// network problem.
	if timeout <= 0 {
		timeout = defaultSinkTimeout
	}

	return &Service{
		// Copied, so the caller keeping and reusing its slice cannot race a
		// post in flight or swap in a sink this Service was never given. One
		// clone at construction against a data race and an FR-016 hole —
		// "disabled sinks are not invoked" is only true if the set cannot
		// change underneath us.
		sinks:     slices.Clone(sinks),
		timeout:   timeout,
		recording: recording,
		newID:     generateID,
	}
}

// Outcome is one submission and everything it produced.
//
// data-model.md calls this entity Post. It is named Outcome here because
// post.Post stutters and because the type is the *result* of a submission
// rather than the submission itself — Service.Post performs it, this records
// what happened. data-model.md's Post also carries a Source field, which is
// deliberately absent: the front door that knows the source does not read it
// back off this type, the log record's source comes from logging.Options, and
// an unused field would be a claim this batch cannot test. Both differences are
// amended in data-model.md by this batch.
type Outcome struct {
	// ID correlates every log record for this post (FR-066, R-007). Generated
	// once, here, so both front doors and every sink event agree on it.
	ID ulid.ULID

	// Message is what was submitted, exactly as the user typed it (FR-012).
	Message Message

	// Results holds one entry per sink, in the order the sinks were given.
	//
	// Order is stable on purpose even though the sinks run concurrently: a
	// front door renders these in sequence, and a report whose lines reorder
	// between runs is harder to read and harder to test than one that does
	// not.
	Results []SinkResult
}

// Succeeded reports whether the post as a whole succeeded (FR-059).
//
// It delegates to AllSucceeded rather than reimplementing the rule, so the
// empty-slice decision recorded there — false, not the vacuous true — holds
// here too.
func (o Outcome) Succeeded() bool {
	return AllSucceeded(o.Results)
}

// Post delivers message to every sink concurrently and returns once all of
// them have finished (FR-012 - FR-016).
//
// message must already have passed Message.Validate. This type does not
// validate, because a whitespace-only submission is not a post that failed —
// it is a post that must never start, and the user needs FR-011's message from
// the front door rather than a result report saying every sink failed. T037
// and T044 reject before they reach here.
//
// Every sink is started before any is awaited, so they overlap (FR-013), and
// every result is collected before returning, so a failure never truncates the
// report (FR-014). There is no path that returns early: the only return is
// after the WaitGroup drains.
func (s *Service) Post(message Message) Outcome {
	// Before the identifier, so a post whose ULID generation is slow is not
	// credited with less time than it took. The whole post's elapsed time is
	// FR-066's duration_ms on the terminal record.
	started := time.Now()

	outcome := Outcome{ID: s.identifier(), Message: message}

	// One recorder for the whole post, obtained once and shared by every
	// delivery goroutine. Sharing it is what makes the records correlate: the
	// identifier is bound at construction, so no call site can emit a record
	// belonging to a different post or to no post at all.
	recorder := s.recorderFor(outcome.ID.String())

	// Emitted before anything can fail, because FR-067 makes this the record
	// that says a post existed. A submission that then panicked or hung in
	// every sink still has to be reconstructable (SC-008), and it is only
	// reconstructable from an arrival record admitted before delivery. The
	// production recorder queues I/O; a stalled or crashed logger can lose
	// diagnostics, but must never hold up the destinations.
	recorder.MessageReceived(message)

	if len(s.sinks) == 0 {
		// Not an error state to report here. FR-018 makes settings with every
		// sink disabled a startup error that the front door raises before a
		// post is attempted, so this is unreachable in a wired application.
		// Returning an Outcome with no results is the fail-closed answer for
		// the path that reaches it anyway: AllSucceeded is false for an empty
		// slice, so the exit status says nothing was delivered.
		recorder.PostCompleted(outcome, time.Since(started))

		return outcome
	}

	// One slot per sink, each written by exactly one goroutine and never read
	// until the WaitGroup has drained. That is what makes this race-free
	// without a mutex, and it is also what keeps Results in the sinks' order:
	// appending from the goroutines would order them by completion, so the
	// report would reshuffle whenever the network was slow.
	results := make([]SinkResult, len(s.sinks))

	var running sync.WaitGroup

	for i, sink := range s.sinks {
		running.Add(1)

		go func(index int, sink Sink) {
			defer running.Done()

			results[index] = s.run(recorder, sink, message)
		}(i, sink)
	}

	running.Wait()

	outcome.Results = results

	// After the wait, so the terminal record cannot precede a sink record that
	// belongs to the same post. An abandoned delivery goroutine is the one
	// exception and it is unavoidable: it may still be inside a sink when this
	// runs, and its own records were already emitted by run's backstop, so what
	// arrives late is nothing. sinkRecord is what guarantees that.
	recorder.PostCompleted(outcome, time.Since(started))

	return outcome
}

// run bounds one sink's delivery, whatever the sink does, and is the only
// thing Post's goroutines call.
//
// The per-sink context is handed to the sink and is the primary mechanism, but
// it is only an *offer*: Send has to consult it. A sink doing a blocking
// syscall cannot, and that is not a hypothetical — the obsidian sink (T031)
// writes with os.OpenFile and os.File.Write, neither of which takes a context,
// so a vault on a synced or network-backed path (iCloud Drive, Dropbox, SMB,
// sshfs) blocks for as long as the mount does, and a hard-mounted dead export
// blocks forever. Before this backstop existed, such a sink held the whole post
// open past its deadline with no upper bound, and its sibling's already-finished
// result stayed unreachable the entire time. That is one sink deciding another's
// reported outcome, which principle I forbids, arriving by the one route the
// panic guard does not cover.
//
// So delivery runs in its own goroutine and this one stops waiting after the
// deadline plus a grace period. Two consequences are accepted deliberately
// rather than hidden:
//
//   - The abandoned goroutine outlives the post. It is one goroutine per hung
//     submission, it holds no lock the caller needs, and its sends cannot block
//     because both channels are buffered — so nothing the caller does depends
//     on it ever finishing. Three honest caveats. It is not necessarily
//     *parked*: a sink returning an error whose Unwrap cycles leaves it
//     spinning inside errors.Is, burning a full core until the process exits.
//     Nothing caps the count, so a long-lived GUI session posting repeatedly at
//     a dead mount accumulates one per attempt along with one retained copy of
//     each message. And the cost is ~4.9 KiB per abandoned sink — 0.9 KiB of
//     heap plus a 4.1 KiB goroutine stack, measured over 500 abandoned posts.
//     An earlier version of this note said ~0.7 KiB, which counted only the
//     heap and omitted the stack: the larger term, and the resource this design
//     actually chooses to leak. The goroutines are fully reclaimed once the
//     sink unblocks.
//   - A write that the sink completes after we stopped waiting still lands. The
//     user is told the sink timed out and a note may appear in the vault
//     afterwards. Reporting a timeout for work that was abandoned is the honest
//     description of what this process knows; the alternative was a front door
//     that hangs with no report at all. Narrower than it reads for the sink
//     that motivated it, in the safe direction: the obsidian sink consults the
//     context again after its open and before its write, so an append abandoned
//     while the open blocked — the stalled-mount case — writes nothing, and
//     only one abandoned inside the write itself becomes a phantom entry. The
//     cost is stated at its widest because Sink is an interface and no
//     implementation is obliged to check anything.
func (s *Service) run(rec Recorder, sink Sink, message Message) SinkResult {
	// Before anything the sink can influence, so a slow Name() is counted in
	// Duration rather than excluded from it.
	started := time.Now()

	// The arbiter for this sink's two records. Both this goroutine and the
	// delivery goroutine can reach it — the backstop below finishes an
	// abandoned sink, and the abandoned goroutine finishes itself if it ever
	// returns — and it is what makes "one start and one finish per sink" true
	// rather than usually true.
	record := newSinkRecord(rec)

	// Both buffered, so the abandoned goroutine's sends always complete and
	// the goroutine can exit. An unbuffered channel would park it forever on a
	// receive nobody is going to make.
	//
	// named exists so that abandoning a sink does not also mean losing its
	// name. Name() is resolved inside the goroutine — it is sink code and has
	// to be bounded like the rest — but the abandonment branch still wants it,
	// and the case the whole backstop was built for is a sink whose Name()
	// works fine and whose Send() hangs. Publishing it as soon as it is known
	// keeps that report attributable.
	var (
		named = make(chan string, 1)
		done  = make(chan SinkResult, 1)
	)

	go func() {
		// The send is deferred, and the value it sends is seeded with the
		// abnormal-exit failure before delivery starts.
		//
		// runtime.Goexit terminates this goroutine after running its deferred
		// functions but without completing the statement it was in, so a plain
		// `done <- s.deliver(...)` never sends at all: run would then wait out
		// the whole timer and report a timeout for a sink that had already
		// stopped. A deferred send always happens, and on that path it carries
		// this seed rather than a value deliver never returned. The seed starts
		// under the sentinel because at this point the sink has not been asked
		// its name yet — a Goexit inside Name() must still produce a result
		// that names something.
		outcome := SinkResult{
			Name:    unknownSinkName,
			Success: false,
			Reason:  reasonFailed,
			Err:     errAbnormalExit,
		}

		delivered := false

		defer func() {
			// deliver sets Duration on every path it returns through. It does
			// not return through any of them on a Goexit, so its value is
			// discarded and the seed above would report a duration of zero for
			// work that took real time — which FR-066 turns into a
			// duration_ms of 0.
			if !delivered {
				outcome.Duration = time.Since(started)
			}

			done <- outcome
		}()

		name, namePanic := s.nameOf(sink)

		// Published to the record before the start event can be emitted, so
		// every record for this sink names the same destination — including the
		// one the backstop writes for a sink it abandoned.
		record.named(name, namePanic)

		named <- name
		outcome.Name = name

		outcome = s.deliver(record, name, sink, message, started)
		delivered = true
	}()

	timer := time.NewTimer(enforcementBound(s.timeout))
	defer timer.Stop()

	select {
	case result := <-done:
		record.finish(result)

		return result
	case <-timer.C:
		name := resolvedName(named)

		// Recorded from here rather than left to the abandoned goroutine.
		// FR-070 wants every failure logged, and this one is a failure the
		// orchestrator caused: waiting for the goroutine to report it would
		// mean the record arrived after the post's own terminal record, or on a
		// hard-mounted dead export never at all.
		result := SinkResult{
			Name:    name,
			Success: false,
			Reason:  reasonTimedOut,
			// Wrapping context.DeadlineExceeded keeps one classification for
			// both routes to a timeout: whether the sink noticed its deadline
			// or had to be abandoned, errors.Is says the same thing.
			Err: fmt.Errorf("sink %s did not return within %s and was abandoned: %w",
				name, enforcementBound(s.timeout), context.DeadlineExceeded),
			Duration: time.Since(started),
		}

		record.finish(result)

		return result
	}
}

// enforcementBound is how long run waits before abandoning a sink.
//
// The addition is saturating, and that is not defensive tidiness: adding the
// grace to a timeout near MaxInt64 wraps to a large negative duration,
// time.NewTimer fires a negative duration immediately, and every post then
// reported "did not return within -2562047h..." while its deliveries carried
// on and landed. New's floor catches the negative half of that overflow class
// and this catches the positive half — a huge timeout means "effectively
// unbounded", which is what MaxInt64 gives.
//
// The upper bound on posting.sink_timeout_seconds still belongs in config's
// validation, which today checks only that it is above zero.
func enforcementBound(timeout time.Duration) time.Duration {
	if timeout > math.MaxInt64-enforcementGrace {
		return math.MaxInt64
	}

	return timeout + enforcementGrace
}

// resolvedName returns the sink's name if the delivery goroutine got as far as
// resolving it, and the sentinel otherwise.
//
// Non-blocking on purpose: this runs on the abandonment path, and the one shape
// that leaves the name unresolved is a Name() that is itself stuck — waiting
// for it here would reintroduce exactly the unbounded wait being escaped.
func resolvedName(named <-chan string) string {
	select {
	case name := <-named:
		return name
	default:
		return unknownSinkName
	}
}

// nameOf reads a sink's name without letting it take down the post.
//
// Name() is sink code, and this is called from inside the delivery goroutine so
// that it is bounded by the same backstop as Send.
//
// The recovered value is kept and returned (issue #110). It used to be
// discarded here, which made a sink that panicked in Name while delivering
// successfully invisible forever: the result was a clean success and the panic
// value was destroyed at the layer that recovered it, so no later task could
// record what FR-071 requires. It travels to the recorder as
// SinkAttempt.NameErr, wrapped in the same panicError deliver uses for a
// panicking Send, so both panics render through one guarded description.
//
// One shape still leaves nothing: panic(nil) under GODEBUG=panicnil=1 arrests
// the panic while recover returns nil, so there is no value to keep. The name
// is still corrected to the sentinel, which is the part that keeps the record
// well formed.
//
// Both properties were learned the hard way, one review each. First, Name was
// called outside any recover, so a panicking Name — or a nil element in the
// slice T036 builds — killed the process along with the sibling's result. That
// added the recover here. Then Name was still being called synchronously in
// run, before the goroutine and before the timer existed, so a *blocking*
// Name reproduced the unbounded hang the backstop had just been built to fix,
// and a Goexit inside it produced a result naming nothing at all. Two
// interface methods; both are sink code; both need the same treatment.
func (s *Service) nameOf(sink Sink) (name string, namePanic error) {
	defer func() {
		// The recovered value is captured, and the name is still corrected
		// without consulting it.
		//
		// Branching on the recovered value to decide the *name* was wrong twice
		// over. panic(nil) under panicnil=1 arrests a panic while returning
		// nil, so the guard did not fire and the zero value came back — a blank
		// name, which is exactly what this sentinel exists to prevent. And a
		// sink that simply returns "" needs no panic at all to reach the same
		// place. Deciding the name from the name covers every route out of
		// here, including ones nobody has thought of yet, and the value below
		// only ever adds a diagnostic.
		recovered := recover()

		if name == "" {
			name = unknownSinkName
		}

		// After the correction, so the diagnostic names the same sink the
		// records do. A panic deferred *after* Name set its result leaves both
		// a usable name and a recovered value, which is the one shape where
		// these two are not the sentinel and a diagnostic at once.
		if recovered != nil {
			namePanic = &panicError{sink: name, value: recovered}
		}
	}()

	// Assigned to the named result rather than returned directly, so the
	// deferred correction above sees the value the sink produced.
	name = sink.Name()

	return name, nil
}

// deliver runs one sink under its own deadline and converts whatever happens
// into a SinkResult.
//
// The context is derived from context.Background() and from nothing else. This
// is the line that enforces constitution principle I: a shared cancellable
// parent would let the first sink's failure cancel its sibling, and every
// guarantee about independence — FR-013's run-to-completion, FR-014's
// report-everything, FR-070's log-every-failure — would silently become
// conditional on which sink failed first. The parent is not a parameter for the
// same reason; there is no caller-supplied context to accidentally pass in.
//
// A panic in a sink becomes a failure result rather than taking down the
// process. FR-071 expects panics to be recorded, which means they are
// anticipated, and a panic crossing this boundary would kill the goroutine, the
// sibling's report and the exit status together — exactly the coupling
// principle I forbids, arriving by a route no context discipline can prevent.
//
// name and started are passed in rather than computed here because run already
// needed both: it has to be able to attribute a timeout to a sink it abandoned,
// and the clock has to start before Name() is called so that a slow Name() is
// counted in Duration rather than excluded from it.
func (s *Service) deliver(record *sinkRecord, name string, sink Sink, message Message, started time.Time) (result SinkResult) {
	// Failure-shaped before anything runs, for the exit that stops a panic
	// without giving recover anything to work with.
	//
	// panic(nil) under panicnil=1 is that case: recover() returns nil, so the
	// handler below does not fire, but the panic is arrested and this function
	// returns its named result — which would otherwise be a failure with no
	// name, no reason for the front door and no error for the log. The
	// runtime.Goexit case is handled a level up, in run, because Goexit
	// discards this return value entirely.
	result = SinkResult{Name: name, Success: false, Reason: reasonFailed, Err: errAbnormalExit}

	defer func() {
		if recovered := recover(); recovered != nil {
			result = SinkResult{
				Name:    name,
				Success: false,
				Reason:  reasonFailed,
				Err:     &panicError{sink: name, value: recovered},
			}
		}

		// One place, so every path — success, error, panic, abnormal exit —
		// reports the time actually spent.
		result.Duration = time.Since(started)
	}()

	// Derived from started, not from now, so the sink's deadline and run's
	// backstop are measured from the same origin.
	//
	// With WithTimeout the context began when deliver was entered — after
	// nameOf had run — while the backstop timer started before it. A Name()
	// costing more than the grace therefore pushed the sink's own deadline
	// past the backstop, and a sink that honoured cancellation was abandoned
	// anyway: the diagnostic said "abandoned" for a sink that had done nothing
	// wrong, and a very slow Name lost the attribution too. One origin makes
	// the grace mean what its comment says it means, whatever Name costs.
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(s.timeout))
	defer cancel()

	// The reporter is installed for every sink, whether or not the sink
	// declares that it uses one: a sink that reports without declaring still
	// gets its destination onto its records, and the alternative — installing
	// it only for declared reporters — would make the declaration decide
	// correctness rather than only ordering.
	ctx = WithTargetReporter(ctx, record.start)

	// A sink that reports a destination has its start event emitted at the
	// report, from inside Send, so the record carries the path (issue #98).
	// One that does not gets it here, before Send, because holding it back
	// would mean waiting for a report that never comes: the record would then
	// be written after the send it describes had already finished, and a send
	// interrupted by a killed process would leave no trace of having begun.
	if _, reports := sink.(TargetReporting); !reports {
		record.start("")
	}

	if err := sink.Send(ctx, message); err != nil {
		return SinkResult{Name: name, Success: false, Reason: reasonFor(err), Err: err}
	}

	return SinkResult{Name: name, Success: true}
}

// identifier returns this post's correlation identifier, falling back to the
// zero ULID if one cannot be generated.
//
// ulid.Make would be the one-line version and is not used, because it can
// panic: it calls MustNew, whose entropy source returns ErrMonotonicOverflow
// when more identifiers are drawn in a single millisecond than the 80 entropy
// bits allow. The package's own comment on Make says that cannot happen; the
// overflow path in monotonic entropy says otherwise. Reaching it needs on the
// order of 2^48 identifiers in one millisecond, so this is not a defence
// against a real workload — it is a refusal to put a panic on the posting path
// at all, when the alternative costs one branch.
//
// The fallback is the zero ULID rather than an error return, because a post
// that reached its sinks must not be turned into a non-post by a failure to
// name it. It renders as 26 zeros, which is a valid ULID string and an obvious
// one, so a record carrying it is greppable rather than merely wrong.
func (s *Service) identifier() ulid.ULID {
	id, err := s.newID()
	if err != nil {
		return ulid.ULID{}
	}

	return id
}

// generateID draws a monotonic ULID from the package's default entropy
// (R-007).
func generateID() (ulid.ULID, error) {
	return ulid.New(ulid.Now(), ulid.DefaultEntropy())
}

// panicError carries a recovered sink panic into SinkResult.Err.
//
// A concrete type rather than fmt.Errorf so a caller can tell a panic from an
// ordinary failure without matching on message text, and so the recovered value
// itself survives on the struct for whatever records FR-071's trace (T073).
// No accessor for it yet: T073 can add one when it has a use, and an exported
// getter nothing calls would be a claim this batch cannot test.
//
// SinkResult's render guards keep this out of any display string, and the
// Unwrap-less shape is deliberate: there is nothing underneath to unwrap, and
// a recovered value is not necessarily an error at all.
type panicError struct {
	sink  string
	value any
}

func (e *panicError) Error() string {
	return "sink " + e.sink + " panicked: " + describePanic(e.value)
}

// describePanic renders a recovered value without assuming it is an error or a
// string.
//
// recover() returns any, and a panic value is whatever was passed to panic():
// an error, a string, a fmt.Stringer, or a struct with no rendering at all.
// fmt.Sprint would handle all of them, and is deliberately not used — a
// panicking sink's value can be a struct holding a credential, and %v walks
// exported fields. The three shapes worth reading are handled explicitly and
// everything else is reported by type alone, which says what happened without
// printing state nobody vetted.
func describePanic(value any) (description string) {
	// Rendering a panic value calls a method on it, and that method is the
	// code that was already panicking. A typed-nil pointer to a sink-defined
	// error type is the ordinary shape: Error() dereferences the receiver and
	// faults. Unguarded, that turned this package's recovery into a second,
	// unrecovered panic — raised not here but wherever the diagnostic record
	// is assembled (T040, T073), on the deliberate Err.Error() access FR-017
	// reserves for the log, after deliver had already preserved every result.
	//
	// %T on the value is safe: it reads the type, never the value.
	//
	// One class is beyond reach and is not claimed: a rendering that recurses
	// without a base case exhausts the goroutine stack, and stack exhaustion is
	// a fatal runtime error that no recover can stop. This guard covers panics,
	// not the runtime running out of room to raise one.
	defer func() {
		if recover() != nil {
			description = fmt.Sprintf("a value of type %T whose own rendering panicked", value)
		}
	}()

	switch v := value.(type) {
	case error:
		return v.Error()
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("a value of type %T", value)
	}
}
