package post

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Display reasons this batch can produce.
//
// contracts/cli-interface.md and FR-029 require Reason to come from a fixed
// set, and T056 implements that set in reason.go. These two are the subset
// FR-015 forces on this batch: a sink that exceeds its own deadline has to
// yield a failure result here, and a result with an empty Reason would give
// both front doors nothing to show between this batch and T056.
//
// Neither is derived from the sink's error, and that is the invariant T056
// inherits rather than a temporary shortcut. FR-017 and FR-029 split Reason
// from Err precisely because an error chain carries a URL, a header or a
// credential: a *url.Error from a Telegram transport failure has the bot token
// in its URL field. Reason is the only half a front door may print, so it can
// only ever be a constant chosen by this package.
const (
	reasonTimedOut = "request timed out"
	reasonFailed   = "delivery failed"
)

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

	// newID generates the per-post correlation identifier. A field rather than
	// a direct call so that the failure branch below is reachable from a test;
	// see export_test.go. Nothing outside this package can replace it.
	newID func() (ulid.ULID, error)
}

// New returns a Service that posts to sinks, allowing each one timeout for its
// entire operation (FR-015).
//
// The timeout is per sink and not a budget for the post as a whole: two sinks
// each get the full duration, measured independently from the moment that sink
// starts.
func New(sinks []Sink, timeout time.Duration) *Service {
	return &Service{
		sinks:   sinks,
		timeout: timeout,
		newID:   generateID,
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
	outcome := Outcome{ID: s.identifier(), Message: message}

	if len(s.sinks) == 0 {
		// Not an error state to report here. FR-018 makes settings with every
		// sink disabled a startup error that the front door raises before a
		// post is attempted, so this is unreachable in a wired application.
		// Returning an Outcome with no results is the fail-closed answer for
		// the path that reaches it anyway: AllSucceeded is false for an empty
		// slice, so the exit status says nothing was delivered.
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

			results[index] = s.deliver(sink, message)
		}(i, sink)
	}

	running.Wait()

	outcome.Results = results

	return outcome
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
func (s *Service) deliver(sink Sink, message Message) (result SinkResult) {
	name := sink.Name()
	started := time.Now()

	defer func() {
		if recovered := recover(); recovered != nil {
			result = SinkResult{
				Name:     name,
				Success:  false,
				Reason:   reasonFailed,
				Err:      &panicError{sink: name, value: recovered},
				Duration: time.Since(started),
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	err := sink.Send(ctx, message)
	elapsed := time.Since(started)

	if err != nil {
		return SinkResult{
			Name:     name,
			Success:  false,
			Reason:   reasonFor(err),
			Err:      err,
			Duration: elapsed,
		}
	}

	return SinkResult{Name: name, Success: true, Duration: elapsed}
}

// reasonFor picks the display reason for a sink's error.
//
// T056 replaces this with the full classification in reason.go. The shape it
// has to keep is that the result is chosen from constants in this package and
// never built from err: see the comment on reasonTimedOut.
//
// The deadline case is distinguished now because FR-015 is one of this batch's
// requirements and "the sink took too long" is the one failure the orchestrator
// causes rather than observes. errors.Is rather than a comparison, because a
// sink that wraps the context error on its way out still timed out.
func reasonFor(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return reasonTimedOut
	}

	return reasonFailed
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
func describePanic(value any) string {
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
