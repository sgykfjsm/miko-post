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

// TargetReporting is an optional interface a Sink implements to declare that it
// resolves a destination for each post and reports it through ReportTarget.
//
// It exists because contracts/log-events.md requires a path field on all three
// obsidian_append_* events while Sink is deliberately two methods, so an
// orchestrator holding a Sink has no route to the resolved path — and for the
// start event there is no return value yet, so a typed error could not carry it
// either (issue #98).
//
// The method takes nothing and returns nothing, and that is the design rather
// than an oversight. The destination travels per call through the context
// (ReportTarget, decision DEC-D3), because a method that *returned* it could
// only ever return the sink's most recent target: a Sink is built once and
// serves every post, so two overlapping posts overwrite one field and a
// completed post can read a path naming a note it never touched. That is issue
// #111, and it is what this interface's predecessor shipped — a Targeter whose
// Target() the orchestrator read after Send. What the orchestrator needs from
// the type system is therefore not the value but one bit: whether to hold this
// sink's start event for a report that is coming.
//
// A sink with no destination to report simply does not implement it. Its events
// carry no path, which is why chat events have none (issue #98), and its start
// event is emitted before Send rather than waiting for a report that will never
// arrive.
//
// Additive on purpose: Sink stays exactly Name and Send (issue #98), so a sink
// with no meaningful destination is unaffected and a third sink cannot acquire
// different event semantics by accident (constitution principle II).
//
// The rejected alternative is worth recording, because it looks equivalent and
// is not. Having the orchestrator re-derive the path from settings would make
// both sides call time.Now() independently, and a post spanning local midnight
// would then log a path that is not the file that was appended to — the exact
// reconstruction trail SC-008 and constitution principle III exist to
// guarantee.
type TargetReporting interface {
	// ReportsTarget declares that Send calls ReportTarget with the destination
	// it resolved, before performing any I/O on it.
	//
	// Never invoked by the orchestrator. Only the type assertion is consulted,
	// so an implementation's body is irrelevant and by convention empty.
	ReportsTarget()
}
