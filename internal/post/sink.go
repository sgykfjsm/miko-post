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
