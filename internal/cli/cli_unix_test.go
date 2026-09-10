//go:build unix

package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/cli"
)

// TestANamedPipeAtTheSettingsPathExitsRatherThanHanging is ADV-001 through the
// whole front door.
//
// `-c <FIFO>` used to hang: the message validated, load called os.ReadFile,
// and os.ReadFile blocked inside open(2) waiting for a writer that was never
// coming. Measured as rc=124 under a timeout with no output at all — not a
// slow post, not a bad message, nothing on either stream. This is the one
// failure the front door cannot report, because it never gets to the reporting.
//
// The asymmetry is what makes it a front-door test and not only a settings
// one. The log path has had this guard since batch 4 and degrades correctly —
// one warning, the post runs — while the settings path, reachable from the
// same command line, stopped the program dead.
//
// Unix-only, because syscall.Mkfifo does not exist on Windows and a FIFO is the
// cheapest object that blocks on open. Following
// internal/logging/logger_unix_test.go and internal/config/load_unix_test.go,
// which guard their own FIFO cases the same way.
//
// Run in a goroutine against a deadline, for those files' reason: a regression
// does not fail this test, it hangs it, and a timeout panic naming a goroutine
// parked in openat is a much worse diagnosis than an assertion.
func TestANamedPipeAtTheSettingsPathExitsRatherThanHanging(t *testing.T) {
	v := newVault(t)

	// The vault's own settings file is replaced in place, so everything else
	// about the fixture — the writable note directory, the log path — is the
	// arrangement that does post successfully in the tests above. What differs
	// is only what -c points at.
	if err := os.Remove(v.configPath); err != nil {
		t.Fatalf("remove the settings file: %v", err)
	}

	if err := syscall.Mkfifo(v.configPath, 0o600); err != nil {
		t.Fatalf("create the FIFO in the settings file's place: %v", err)
	}

	invocation, err := cli.Parse([]string{"-c", v.configPath, "hello"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	type result struct {
		status int
		out    string
		errOut string
	}

	done := make(chan result, 1)

	go func() {
		// Buffers owned by this goroutine until it sends them, so nothing
		// reads them while Run is still writing.
		var out, errOut bytes.Buffer

		status := cli.Run(invocation, &out, &errOut)
		done <- result{status: status, out: out.String(), errOut: errOut.String()}
	}()

	// Generous: this is measuring "returns at all", not how fast. A blocking
	// open waits forever, so no threshold in between is meaningful.
	const deadline = 10 * time.Second

	var got result

	select {
	case got = <-done:
	case <-time.After(deadline):
		// The goroutine is parked in open(2) and stays parked; there is nothing
		// to cancel. Returning leaks it for the rest of the run, which is the
		// lesser problem.
		t.Fatalf("Run did not return within %s for a named pipe at the settings path", deadline)
	}

	if got.status != cli.ExitFailure {
		t.Errorf("status = %d, want %d", got.status, cli.ExitFailure)
	}

	// Non-zero alone is not enough. The old behaviour was no output at all, and
	// a refusal the user cannot read is barely better than a hang.
	if !strings.Contains(got.errOut, "named pipe") {
		t.Errorf("stderr does not say what is at the settings path: %q", got.errOut)
	}

	if !strings.Contains(got.errOut, v.configPath) {
		t.Errorf("stderr does not name the settings path: %q", got.errOut)
	}

	if got.out != "" {
		t.Errorf("a settings failure printed a report: %q", got.out)
	}

	// FR-010's ordering, still true on this path: nothing was posted and no
	// diagnostics were opened, because the failure returns before either
	// exists.
	if names := v.notes(t); len(names) != 0 {
		t.Errorf("a settings failure still posted: %v", names)
	}

	if _, err := os.Stat(filepath.Dir(v.logPath)); err == nil {
		t.Error("a settings failure created the log directory")
	}
}
