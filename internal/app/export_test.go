package app

import "github.com/sgykfjsm/miko-post/internal/logging"

// ProducibleEvents exposes the event names the recorder can emit, so the
// completeness check against logging.AllEvents can live in the external test
// package with the rest of this package's tests.
//
// A seam rather than an exported function, because nothing outside this package
// has any use for the set: it is an assertion about this adapter's coverage of
// a vocabulary, not a capability. It exists because internal/logging's own
// registration test scans that package's source and cannot see this one, so
// without this check an event name defined there and never produced here would
// be a saved log query that silently returns nothing (decision DEC-D2).
func ProducibleEvents() []logging.Event { return producibleEvents() }

// LifecycleFor exposes the sink-name lookup, so a test can assert that an
// unregistered name is reported rather than mapped to something plausible.
func LifecycleFor(sink string) (started, succeeded, failed logging.Event, ok bool) {
	events, ok := lifecycleFor(sink)

	return events.started, events.succeeded, events.failed, ok
}

// ErrorMessage exposes the guarded error render, whose interesting inputs — a
// typed nil, and an error whose own Error method panics — cannot be produced
// through a sink from outside this package.
func ErrorMessage(err error) string { return errorMessage(err) }

// HTTPStatus exposes the chat-status extraction.
func HTTPStatus(err error) (int, bool) { return httpStatus(err) }

// CaptureProbe exposes the two pieces of per-post capture state whose guards
// cannot be reached through a sink.
//
// Both guards exist for a wiring bug rather than for a user-reachable
// condition, which is exactly why they need a seam: a guard whose branch no
// test can enter is a guard nobody has ever executed, and this repository has
// already been bitten by that shape twice in internal/logging. See
// TestClaimBodyWithNoBodyEmitsNothing and TestKeepTraceKeepsTheFirstTrace.
type CaptureProbe struct{ r *recorder }

// NewCaptureProbe returns a probe over a recorder with no logger attached.
//
// The recorder's log field is left nil, which is safe because neither method
// under test emits anything: claimBody and keepTrace only read and write the
// per-post state that the emitting methods then attach to a record.
func NewCaptureProbe(messageOnErrorOnly bool) *CaptureProbe {
	return &CaptureProbe{r: &recorder{messageOnErrorOnly: messageOnErrorOnly}}
}

// SetBody sets the captured body, as MessageReceived would.
func (p *CaptureProbe) SetBody(body string) { p.r.body = body }

// ClaimBody returns what a failure record would carry, and whether the
// recorder now considers the body emitted.
func (p *CaptureProbe) ClaimBody() (string, bool) {
	body := p.r.claimBody()

	return body, p.r.bodyEmitted
}

// KeepTrace offers a trace, and returns the trace the terminal record would
// carry afterwards.
func (p *CaptureProbe) KeepTrace(trace string) string {
	p.r.keepTrace(trace)

	return p.r.noteTrace
}
