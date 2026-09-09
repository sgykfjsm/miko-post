package post

import "context"

// Sink is a named delivery target.
//
// The interface is deliberately two methods. Everything else a post needs —
// the per-sink timeout, turning a returned error into a SinkResult, and
// emitting the start, success, and failure events — belongs to the orchestrator
// (constitution principle II). A sink that owned its own timeout or built its
// own result could quietly acquire different semantics from its sibling, and
// FR-015's guarantee that one sink's expiry never aborts the post would then
// have to be re-established for each implementation.
type Sink interface {
	// Name is the sink's stable identifier, such as "telegram" or "obsidian".
	// It appears in results shown to the user and in log records, so it is
	// part of the contract rather than a debugging label.
	Name() string

	// Send delivers the message, returning nil on success.
	//
	// The message arrives exactly as the user typed it (FR-012); a sink may
	// transform it for its own storage format but must not treat the
	// transformed form as the message.
	//
	// ctx carries this sink's own overall deadline, derived independently of
	// every other sink (FR-015). Send must honour it and must not assume
	// cancellation says anything about a sibling: no sink is ever cancelled
	// because another failed (constitution principle I).
	Send(ctx context.Context, message Message) error
}

// Targeter is an optional interface a Sink may implement to report the
// destination it resolved for the current post.
//
// It exists because contracts/log-events.md requires a `path` field on the
// three obsidian append events while Sink is deliberately two methods, so an
// orchestrator holding a Sink has no route to the resolved path — and for the
// `_started` event there is no return value yet, so a typed error could not
// carry it either. The orchestrator type-asserts this and adds `path` only when
// it is satisfied, which is why the telegram sink does not implement it and why
// chat events carry no path (issue #98).
//
// Additive on purpose: Sink stays exactly Name and Send, so a sink that has no
// meaningful target is unaffected and a third sink cannot acquire different
// event semantics by accident (constitution principle II).
//
// The rejected alternative is worth recording, because it looks equivalent and
// is not. Having the orchestrator re-derive the path from settings would make
// both sides call time.Now() independently, and a post spanning local midnight
// would then log a `path` that is not the file that was appended to — the exact
// reconstruction trail SC-008 and constitution principle III exist to
// guarantee.
type Targeter interface {
	// Target is the destination this sink resolved for the current post, such
	// as today's daily-note path.
	//
	// It must return what Send actually resolved and wrote, recorded during
	// Send. An implementation that re-resolves on call reintroduces the
	// midnight mismatch this interface was introduced to avoid.
	Target() string
}
