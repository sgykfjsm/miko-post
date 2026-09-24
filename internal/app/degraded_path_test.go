package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/logging"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
	"github.com/sgykfjsm/miko-post/internal/sink/telegram"
)

// T067, FR-076 and SC-013: an unwritable log path.
//
// internal/app rather than internal/post, which is where T067's text puts it:
// the posting core does not import internal/logging and cannot be handed a
// logger, so "the log could not be opened" is not a condition it can be placed
// in. This package is the first layer that wires the two together, and is
// therefore the first layer where all four of FR-076's clauses are observable
// at once.
//
// recording_liveness_test.go already covers the sibling condition — a writer
// that accepts the handle and then blocks or fails mid-post. This covers the
// one T067 names, where the open itself never succeeds, and asserts the four
// clauses together rather than one per test: FR-076 is a conjunction, and a
// regression that trades one clause for another (a warning that suppresses a
// sink, an outcome corrected to match the log) passes every individual check.

// countingSink records that it ran and reports the outcome it is told to.
type countingSink struct {
	name    string
	target  string
	sendErr error
	calls   atomic.Int64
}

func (s *countingSink) Name() string { return s.name }

func (s *countingSink) Send(ctx context.Context, _ post.Message) error {
	s.calls.Add(1)

	if s.target != "" {
		post.ReportTarget(ctx, s.target)
	}

	return s.sendErr
}

// unopenableLogPath returns a log path whose parent directory cannot be
// created, because a regular file already occupies the parent's name.
//
// A regular file in the way rather than a chmod: FR-075 has logging.Open create
// a missing directory, so the failure has to be one that defeats the creation
// itself. os.MkdirAll on a path whose ancestor is a file fails as ENOTDIR for
// every user including root, where a 0500 directory would still be writable by
// root and would make this test pass vacuously in a container.
func unopenableLogPath(t *testing.T) string {
	t.Helper()

	blocker := filepath.Join(t.TempDir(), "not-a-directory")

	if err := os.WriteFile(blocker, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}

	return filepath.Join(blocker, "logs", "app.jsonl")
}

// TestAnUnwritableLogPathCostsTheLogAndNothingElse is T067.
func TestAnUnwritableLogPathCostsTheLogAndNothingElse(t *testing.T) {
	settings := validSettings(t)
	settings.Logging.Path = unopenableLogPath(t)

	note := &countingSink{name: obsidian.SinkName, target: "today.md"}
	chat := &countingSink{name: telegram.SinkName, sendErr: errSendFailed}

	logger := app.OpenLogger(settings, logging.SourceCLI)

	outcome := post.New([]post.Sink{note, chat}, app.SinkTimeout(settings),
		app.NewRecording(logger, settings.Logging)).
		Post(post.Message{Original: "this must still be delivered"})

	_ = logger.Close()

	// 1. Every enabled destination still runs.
	if got := note.calls.Load(); got != 1 {
		t.Errorf("the note sink ran %d times, want 1; a log that cannot be opened must not stop a destination", got)
	}

	if got := chat.calls.Load(); got != 1 {
		t.Errorf("the chat sink ran %d times, want 1; a log that cannot be opened must not stop a destination", got)
	}

	// 2. The real outcomes are reported — including the failure, which is the
	//    half a logger that swallowed everything could fake by reporting
	//    success.
	if len(outcome.Results) != 2 {
		t.Fatalf("got %d results, want one per destination: %+v", len(outcome.Results), outcome.Results)
	}

	if !outcome.Results[0].Success {
		t.Errorf("the note's real success was not reported: %+v", outcome.Results[0])
	}

	if outcome.Results[1].Success {
		t.Errorf("the chat's real failure was reported as a success: %+v", outcome.Results[1])
	}

	// 3. The exit status is unchanged, which here means it tracks the post and
	//    not the log: one destination failed, so the post did.
	if outcome.Succeeded() {
		t.Error("the post reports overall success although a destination failed")
	}

	// 4. Exactly one warning, naming the path and the reason.
	degraded := logger.Degraded()
	if degraded == nil {
		t.Fatal("no degradation was reported, so the user would never learn the log was lost")
	}

	warning := degraded.Warning()

	// One assertion, not two: the path is never empty here, so
	// strings.Contains already fails on an empty warning. A separate
	// emptiness check could never be the assertion that caught a regression.
	if !strings.Contains(warning, settings.Logging.Path) {
		t.Errorf("the warning does not name the log path, or is empty:\n%s", warning)
	}
}

// TestAnUnwritableLogPathStillDeliversAFullySuccessfulPost is the same
// condition with nothing else wrong.
//
// Separated because the assertion that matters here is the exit status: with no
// destination failing, a post whose log could not be opened must still be a
// successful post. A degradation folded into the outcome would turn a delivered
// message into a failed run, which is precisely what FR-076's "MUST NOT change
// the exit status" forbids.
func TestAnUnwritableLogPathStillDeliversAFullySuccessfulPost(t *testing.T) {
	settings := validSettings(t)
	settings.Logging.Path = unopenableLogPath(t)

	note := &countingSink{name: obsidian.SinkName, target: "today.md"}
	chat := &countingSink{name: telegram.SinkName}

	logger := app.OpenLogger(settings, logging.SourceCLI)

	outcome := post.New([]post.Sink{note, chat}, app.SinkTimeout(settings),
		app.NewRecording(logger, settings.Logging)).
		Post(post.Message{Original: "delivered despite the log"})

	_ = logger.Close()

	if !outcome.Succeeded() {
		t.Errorf("a fully delivered post was reported as failed because the log could not be opened: %+v", outcome.Results)
	}

	if logger.Degraded() == nil {
		t.Error("the log could not be opened and nothing recorded a degradation")
	}
}
