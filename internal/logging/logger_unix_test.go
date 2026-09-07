//go:build unix

package logging_test

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/logging"
)

// TestOpenDoesNotBlockOnANonRegularFile covers the one failure mode FR-076
// cannot absorb (T023, FR-075).
//
// os.OpenFile on a FIFO blocks inside open(2) until a reader attaches. With a
// named pipe at the log path, Open never returned: no degradation, no warning,
// no records — and no post, because the front door was still in Open. That is
// worse than the disk-full case FR-076 was written for, since the whole point
// of FR-076 is that diagnostics never change what the post does.
//
// Unix-only, because a FIFO is the cheapest way to build a file that blocks on
// open and syscall.Mkfifo does not exist on Windows. The guard it tests is not
// platform-specific: os.Stat rejects anything that is not a regular file.
//
// Open runs in a goroutine against a deadline rather than being called
// directly. A regression here does not fail this test, it hangs it — a `go
// test` timeout panic naming a goroutine parked in openat is a much worse
// diagnosis than an assertion, and it would take the whole package's run with
// it.
func TestOpenDoesNotBlockOnANonRegularFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.jsonl")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create the FIFO in the log's place: %v", err)
	}

	type result struct {
		logger   *logging.Logger
		degraded *logging.Degradation
	}

	done := make(chan result, 1)

	go func() {
		logger := logging.Open(logging.Options{Path: path, Source: logging.SourceCLI})
		done <- result{logger: logger, degraded: logger.Degraded()}
	}()

	// Generous: this is measuring "returns at all", not how fast. A blocking
	// open waits forever, so no threshold in between is meaningful.
	const deadline = 10 * time.Second

	var got result

	select {
	case got = <-done:
	case <-time.After(deadline):
		// The goroutine is parked in open(2) and stays parked; there is
		// nothing to cancel. Returning leaks it for the rest of the run, which
		// is the lesser problem.
		t.Fatalf("Open did not return within %s for a named pipe at the log path", deadline)
	}

	if got.degraded == nil {
		t.Fatal("Open reported no degradation for a log path it cannot use")
	}

	// The warning has to say what is actually wrong. "permission denied" or a
	// bare errno would send the user looking at the mode bits.
	if warning := got.degraded.Warning(); !strings.Contains(warning, "named pipe") {
		t.Errorf("the warning does not name what is at the path: %q", warning)
	}

	// FR-076 in full: the post still runs.
	post := got.logger.Post("id")
	post.Info(logging.EventMessageReceived)
	post.Error(logging.EventRequestCompletedWithError)

	if err := got.logger.Close(); err != nil {
		t.Errorf("Close on a degraded logger: %v", err)
	}
}
