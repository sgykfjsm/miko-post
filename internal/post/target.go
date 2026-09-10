package post

import "context"

// TargetReport receives the destination a sink resolved for one post.
//
// The orchestrator installs one per Send call, so the value it carries belongs
// to that call and to no other. That is the whole point of the shape. The
// interface this replaced returned the destination from a method on the sink,
// and a sink is constructed once and serves every post (T036), so the value it
// held was the *sink's* most recent target rather than *this post's* — issue
// #111, where a post that finished while a later one was still inside Send read
// a path naming a note it never touched. A function reached through the context
// cannot have that defect, because it never leaves the goroutine running the
// post that installed it.
type TargetReport func(target string)

// targetReportKey is the context key the report travels under.
//
// An unexported zero-size struct type, which is the standard shape and is load
// bearing here twice over: the key is unforgeable from outside this package, so
// nothing but the orchestrator can install a report, and it cannot collide with
// another package's key whatever string either of them might have chosen.
type targetReportKey struct{}

// WithTargetReporter returns ctx carrying report, for a Sink to reach through
// ReportTarget.
//
// The orchestrator does this for every Send, so a wired application never calls
// it. It is exported because a sink implementation's own tests are the other
// legitimate caller: "Send reports the destination it resolved" is a property of
// the sink and has to be assertable in the sink's package, and an export_test.go
// seam in this package cannot reach there.
func WithTargetReporter(ctx context.Context, report TargetReport) context.Context {
	return context.WithValue(ctx, targetReportKey{}, report)
}

// ReportTarget tells the orchestrator the destination this sink resolved for the
// post it is delivering. It is a no-op when no reporter is installed.
//
// A Sink that resolves a destination must call this once, from inside Send, the
// instant the destination is known and before any I/O on it (decision DEC-D3).
// The orchestrator emits that sink's start event at this call with the
// destination attached, which is what puts contracts/log-events.md's required
// path field on all three obsidian_append_* records (issue #98) rather than only
// on the two that follow the write.
//
// Before any I/O is the part that matters for the failure case, which is the one
// a user needs most: a note that could not even be opened still has to be named,
// because a failure the user cannot locate is most of a failure they cannot act
// on.
//
// Only the first call in a post has an effect. The start event is emitted once,
// and a later report would either duplicate a record or contradict one already
// written; ignoring it keeps one record per lifecycle stage and keeps the
// destination the one the sink committed to first.
//
// Safe from any goroutine, and safe after Send has returned or been abandoned:
// the orchestrator's reporter is mutex-guarded and discards a late report.
func ReportTarget(ctx context.Context, target string) {
	report, ok := ctx.Value(targetReportKey{}).(TargetReport)
	if !ok || report == nil {
		return
	}

	report(target)
}
