package logging_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/logging"
)

// DegradedSoFar exists because Degraded has a side effect that makes it unsafe
// to ask repeatedly, and the GUI asks after every post.
//
// These are that difference, asserted. The defect they exist for was found by
// review: internal/gui called Degraded() once per post, and a single write
// slower than the flush timeout permanently failed the queue — so probing for a
// diagnostics failure caused one, discarding every record of every later post in
// the session.

// stalledWriter blocks the logger's worker for one write, then runs freely.
type stalledWriter struct {
	stall time.Duration
	once  sync.Once

	mu    sync.Mutex
	lines []string
}

func (w *stalledWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { time.Sleep(w.stall) })

	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, string(p))

	return len(p), nil
}

func (w *stalledWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.lines)
}

// TestDegradedSoFarDoesNotDestroyTheQueueOnASlowWrite is the property the GUI
// depends on.
//
// One write slower than the flush timeout, then several more posts. With the
// non-flushing query every later record still reaches the writer; with the
// flushing one the queue is failed permanently and they are all discarded.
func TestDegradedSoFarDoesNotDestroyTheQueueOnASlowWrite(t *testing.T) {
	writer := &stalledWriter{stall: 400 * time.Millisecond}

	logger := logging.Open(logging.Options{Source: logging.SourceGUI, Writer: writer, Path: "/tmp/probe.jsonl"})

	// The post whose write stalls, then the query the GUI makes after it.
	logger.PostAsync("post-1").Info(logging.EventMessageReceived)

	if degraded := logger.DegradedSoFar(); degraded != nil {
		t.Errorf("a slow write was reported as a degradation: %v", degraded.Err)
	}

	// Several more posts, as a session would.
	for _, id := range []string{"post-2", "post-3", "post-4"} {
		logger.PostAsync(id).Info(logging.EventMessageReceived)
		logger.DegradedSoFar()
	}

	// Wait for the worker to drain rather than relying on Close's own flush:
	// Close flushes with the same 250ms bound, so a stall longer than that
	// would make Close the thing that fails the queue and this test would be
	// measuring Close instead of DegradedSoFar.
	deadline := time.Now().Add(5 * time.Second)
	for writer.written() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := writer.written(); got != 4 {
		t.Errorf("the writer received %d records, want 4; the session's diagnostics were discarded", got)
	}
}

// TestDegradedSoFarIsCheapEnoughToAskEveryPost pins that it does not wait on the
// worker.
//
// The flushing query blocks for up to the flush timeout; this one must return
// while a slow write is still in flight, because the GUI calls it on the Fyne
// event goroutine and a window that freezes for a quarter of a second per post
// is a visible defect.
func TestDegradedSoFarIsCheapEnoughToAskEveryPost(t *testing.T) {
	writer := &stalledWriter{stall: 400 * time.Millisecond}

	logger := logging.Open(logging.Options{Source: logging.SourceGUI, Writer: writer, Path: "/tmp/probe.jsonl"})
	defer logger.Close()

	logger.PostAsync("post-1").Info(logging.EventMessageReceived)

	started := time.Now()
	logger.DegradedSoFar()
	elapsed := time.Since(started)

	// Generous: the point is that it does not wait on the worker at all, not
	// that it hits any particular microsecond.
	if elapsed > 100*time.Millisecond {
		t.Errorf("DegradedSoFar blocked for %v while a write was in flight; it must not wait on the worker", elapsed)
	}
}

// TestDegradedSoFarStillReportsAnOpenFailure is the case that matters most in
// practice, and the one that needs no flush at all.
func TestDegradedSoFarStillReportsAnOpenFailure(t *testing.T) {
	// A regular file in the way, so FR-075's directory creation cannot
	// succeed. A merely absent directory would just be created.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}

	logger := logging.Open(logging.Options{
		Source: logging.SourceGUI,
		Path:   filepath.Join(blocker, "logs", "app.jsonl"),
	})
	defer logger.Close()

	degraded := logger.DegradedSoFar()
	if degraded == nil {
		t.Fatal("an unopenable log reported no degradation")
	}

	if !strings.Contains(degraded.Warning(), "app.jsonl") {
		t.Errorf("the warning does not name the log path: %s", degraded.Warning())
	}
}

// TestDegradedSoFarReportsALatchedWriteFailure pins the second of the two
// states it reads.
func TestDegradedSoFarReportsALatchedWriteFailure(t *testing.T) {
	writer := &failingWriter{err: errors.New("write refused")}

	logger := logging.Open(logging.Options{Source: logging.SourceGUI, Writer: writer, Path: "/tmp/probe.jsonl"})
	defer logger.Close()

	// Synchronous, so the failure is latched before the query.
	logger.Post("post-1").Info(logging.EventMessageReceived)

	if logger.DegradedSoFar() == nil {
		t.Error("a write that already failed was not reported")
	}
}
