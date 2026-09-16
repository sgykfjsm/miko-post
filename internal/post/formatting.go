package post

import (
	"context"
	"time"
)

// FormattingAttempt describes one stage of the format fallback, never a retry queue.
type FormattingAttempt struct {
	Plain    bool
	Err      error
	Duration time.Duration
}

// FormattingRecorder is an optional extension for sinks with format fallback.
type FormattingRecorder interface {
	FormattingFinished(SinkAttempt, FormattingAttempt)
}
type formattingKey struct{}

func WithFormattingReporter(ctx context.Context, report func(FormattingAttempt)) context.Context {
	return context.WithValue(ctx, formattingKey{}, report)
}
func ReportFormatting(ctx context.Context, attempt FormattingAttempt) {
	defer swallow()
	if report, ok := ctx.Value(formattingKey{}).(func(FormattingAttempt)); ok && report != nil {
		report(attempt)
	}
}
func (g guarded) FormattingFinished(sink SinkAttempt, attempt FormattingAttempt) {
	defer swallow()
	if rec, ok := g.inner.(FormattingRecorder); ok {
		rec.FormattingFinished(sink, attempt)
	}
}
func (r *sinkRecord) formatting(attempt FormattingAttempt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.startLocked("")
	if rec, ok := r.rec.(FormattingRecorder); ok {
		rec.FormattingFinished(r.attempt, attempt)
	}
}
