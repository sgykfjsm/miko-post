package gui

import (
	"errors"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// T074 and FR-076 for the GUI front door.
//
// Before this batch internal/gui never called Logger.Degraded(), so FR-076's
// warning was structurally unreachable from the window: diagnostics could fail
// for a whole session and the user was never told. Issue #75 carries that
// observation. These tests are the property that fixes it, and the two that
// matter are opposites — the warning appears when diagnostics failed, and it
// appears exactly once.

// degradedLogPath is the path the fake degradation names, distinctive enough to
// assert on.
const degradedLogPath = "/nowhere/diagnostics.jsonl"

// unwritable is a degradation of the shape logging.Open produces when the log
// cannot be opened.
func unwritable() *logging.Degradation {
	return &logging.Degradation{Path: degradedLogPath, Err: errors.New("permission denied")}
}

// succeeds is a post that delivered to both destinations.
func succeeds(post.Message) post.Outcome {
	return post.Outcome{Results: []post.SinkResult{
		{Name: "obsidian", Success: true},
		{Name: "telegram", Success: true},
	}}
}

// fails is a post where one destination failed, which is the case that also
// renders a log path.
func fails(post.Message) post.Outcome {
	return post.Outcome{Results: []post.SinkResult{
		{Name: "obsidian", Success: true},
		{Name: "telegram", Success: false, Reason: "timed out"},
	}}
}

// submitAndSettle runs one post through the window and drains the dispatch
// queue so the result label holds the finished text.
func submitAndSettle(t *testing.T, h *harness, text string) string {
	t.Helper()

	h.w.entry.SetText(text)
	h.w.submit()
	// The posting goroutine dispatches the completion, so this blocks on it
	// rather than polling: harness.drain is the same wait every other test in
	// this package uses.
	h.drain(t)

	return h.w.result.Text
}

// TestTheWindowWarnsWhenDiagnosticsCouldNotBeWritten is T074's core property.
func TestTheWindowWarnsWhenDiagnosticsCouldNotBeWritten(t *testing.T) {
	h := setupDegrading(t, succeeds, unwritable)

	shown := submitAndSettle(t, h, "a thought worth keeping")

	if !strings.Contains(shown, degradedLogPath) {
		t.Errorf("the result does not name the log path; FR-076 requires the warning to name it:\n%s", shown)
	}

	if !strings.Contains(shown, "permission denied") {
		t.Errorf("the result does not give the reason; FR-076 requires it:\n%s", shown)
	}
}

// TestTheWindowWarnsAboutDiagnosticsExactlyOncePerSession is FR-076's "exactly
// one" and SC-013's "exactly once", across the case that makes them differ.
//
// logging.Logger latches its degradation, so Degraded() keeps answering for the
// rest of the session. A window that renders that answer on each post shows the
// warning again on the second post, and the third — which is one failure
// reported many times, not many failures reported once.
func TestTheWindowWarnsAboutDiagnosticsExactlyOncePerSession(t *testing.T) {
	h := setupDegrading(t, succeeds, unwritable)

	first := submitAndSettle(t, h, "first post")
	if !strings.Contains(first, degradedLogPath) {
		t.Fatalf("the first post did not warn at all, so this test cannot see a repeat:\n%s", first)
	}

	second := submitAndSettle(t, h, "second post")
	if strings.Contains(second, degradedLogPath) {
		t.Errorf("the second post warned again about the same degradation:\n%s", second)
	}

	third := submitAndSettle(t, h, "third post")
	if strings.Contains(third, degradedLogPath) {
		t.Errorf("the third post warned again about the same degradation:\n%s", third)
	}
}

// TestAHealthyLogProducesNoWarning is the other side, and the one that stops
// the warning becoming decoration on every result.
func TestAHealthyLogProducesNoWarning(t *testing.T) {
	h := setupDegrading(t, succeeds, func() *logging.Degradation { return nil })

	shown := submitAndSettle(t, h, "a thought worth keeping")

	if strings.Contains(shown, "diagnostic") || strings.Contains(shown, "permission denied") {
		t.Errorf("a healthy log still produced a warning:\n%s", shown)
	}
}

// TestADegradationThatBeginsMidSessionIsStillReported pins the consequence
// degradationWarning's comment accepts.
//
// The open succeeded and a later write failed, which is the shape Degraded()
// exists to report separately from the open. Asking once at construction would
// miss it entirely.
func TestADegradationThatBeginsMidSessionIsStillReported(t *testing.T) {
	healthy := true
	h := setupDegrading(t, succeeds, func() *logging.Degradation {
		if healthy {
			return nil
		}

		return unwritable()
	})

	if shown := submitAndSettle(t, h, "first post"); strings.Contains(shown, degradedLogPath) {
		t.Fatalf("warned while the log was still healthy:\n%s", shown)
	}

	healthy = false

	if shown := submitAndSettle(t, h, "second post"); !strings.Contains(shown, degradedLogPath) {
		t.Errorf("a degradation that began mid-session was never reported:\n%s", shown)
	}
}

// TestTheWarningReplacesTheLogPathLineOnAFailedPost is the pairing FR-076 and
// the CLI already agree on.
//
// A failed post normally points the user at the log for details. When the log
// is the thing that failed, that line is an instruction to read a file which
// does not have them, so the warning takes its place rather than sitting beside
// it — and the user still gets one sentence about the log, not two.
func TestTheWarningReplacesTheLogPathLineOnAFailedPost(t *testing.T) {
	h := setupDegrading(t, fails, unwritable)

	shown := submitAndSettle(t, h, "a thought worth keeping")

	if strings.Contains(shown, "Details: ") {
		t.Errorf("the result still points at a log that could not be written:\n%s", shown)
	}

	if !strings.Contains(shown, degradedLogPath) {
		t.Errorf("the warning is missing, so the user is told nothing about the log:\n%s", shown)
	}

	// The post's own outcome is untouched: FR-076 must not change reported
	// destination outcomes.
	if !strings.Contains(shown, "timed out") {
		t.Errorf("the destination's real outcome was lost:\n%s", shown)
	}
}

// TestTheWarningDoesNotChangeTheExitStatus is FR-076's other two clauses at
// this layer.
//
// A diagnostics failure must not change the reported outcomes or the exit
// status. The window's status is what cmd/mp returns, so a warning that moved
// it would turn a delivered post into a failed run.
func TestTheWarningDoesNotChangeTheExitStatus(t *testing.T) {
	healthy := setupDegrading(t, succeeds, func() *logging.Degradation { return nil })
	submitAndSettle(t, healthy, "a thought worth keeping")

	degraded := setupDegrading(t, succeeds, unwritable)
	submitAndSettle(t, degraded, "a thought worth keeping")

	if healthy.w.status != degraded.w.status {
		t.Errorf("a diagnostics failure changed the exit status: healthy %d, degraded %d",
			healthy.w.status, degraded.w.status)
	}

	if degraded.w.status != 0 {
		t.Errorf("a successful post with a failed log exits %d, want 0", degraded.w.status)
	}
}

// TestNextIsSafeOnAZeroWarning covers the nil and unset shapes directly.
//
// window.warning is a pointer and next is called on every post, so a
// constructor that ever leaves it nil would panic on the first post rather
// than quietly skipping the warning. The nil receiver and the nil function are
// both handled, and both are asserted here rather than left to a reviewer to
// infer from the guard.
func TestNextIsSafeOnAZeroWarning(t *testing.T) {
	var nilWarning *degradationWarning

	if got := nilWarning.next(); got != "" {
		t.Errorf("a nil degradationWarning returned %q", got)
	}

	if got := (&degradationWarning{}).next(); got != "" {
		t.Errorf("a degradationWarning with no logger returned %q", got)
	}
}
