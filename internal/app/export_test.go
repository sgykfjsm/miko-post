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
