//go:build unix

package config_test

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// TestLoadDoesNotHangOnAPathItCannotRead is ADV-001.
//
// os.ReadFile blocks inside open(2) on a named pipe with no writer, and reads a
// character device until an EOF that never arrives. Both were reachable from
// the -c flag: `mp -c <FIFO>` printed nothing and never returned, and
// `mp -c /dev/zero` reached about 1.9 GB of resident memory in a second and
// never returned either. internal/logging.openLogFile had already been given
// this guard, in batch 4, after a FIFO at the log path made logging.Open never
// return; the settings path was the same hole with no degraded mode to fall
// back to.
//
// Unix-only, because a FIFO is the cheapest object that is not a regular file
// and syscall.Mkfifo does not exist on Windows. The guard it tests is not
// platform-specific: os.Stat refuses anything that is not a regular file
// everywhere. Following internal/logging/logger_unix_test.go, which guards its
// own FIFO case the same way.
//
// Load runs in a goroutine against a deadline rather than being called
// directly, for that file's reason: a regression here does not fail this test,
// it hangs it — and a `go test` timeout panic naming a goroutine parked in
// openat, or an out-of-memory kill, is a far worse diagnosis than an assertion,
// and it takes the whole package's run with it.
//
// Two device cases rather than one, and the pair is deliberate. /dev/zero is
// the one that was measured and the one that is dangerous, but a regression
// only shows up there as a deadline — by which point the leaked read has taken
// gigabytes. /dev/null is the same rule with no hazard at all: a regression
// there returns immediately, having decoded an empty document into the
// defaults and reported a settings file that does not configure anything as
// perfectly valid. It is the case that fails fast and says why.
func TestLoadDoesNotHangOnAPathItCannotRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// path builds the thing to point Load at, or returns "" to skip.
		path func(t *testing.T) string
		// kind is the name the refusal has to use, so the message stays
		// actionable rather than becoming a mode string.
		kind string
	}{
		{
			name: "a named pipe blocks in open until a writer attaches",
			path: func(t *testing.T) string {
				t.Helper()

				path := t.TempDir() + "/config.toml"
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatalf("create the FIFO in the settings file's place: %v", err)
				}

				return path
			},
			kind: "a named pipe",
		},
		{
			name: "the null device decodes to an empty document",
			path: func(t *testing.T) string {
				t.Helper()

				if _, err := os.Stat(os.DevNull); err != nil {
					t.Skipf("%s is not available: %v", os.DevNull, err)
				}

				return os.DevNull
			},
			kind: "a device file",
		},
		{
			name: "a character device never reaches EOF",
			path: func(t *testing.T) string {
				t.Helper()

				// Skipped rather than assumed. /dev/zero is present on every
				// system this program supports, and a sandbox that hides it
				// should not turn into a failure about settings.
				if _, err := os.Stat("/dev/zero"); err != nil {
					t.Skipf("/dev/zero is not available: %v", err)
				}

				return "/dev/zero"
			},
			kind: "a device file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.path(t)

			done := make(chan error, 1)

			go func() {
				_, err := config.Load(path)
				done <- err
			}()

			// Generous: this is measuring "returns at all", not how fast. A
			// blocking open waits forever, so no threshold in between is
			// meaningful.
			const deadline = 10 * time.Second

			var err error

			select {
			case err = <-done:
			case <-time.After(deadline):
				// The goroutine is parked in read(2) or open(2) and stays
				// parked; there is nothing to cancel. Returning leaks it for
				// the rest of the run, which is the lesser problem.
				t.Fatalf("Load did not return within %s for %s at the settings path",
					deadline, tt.kind)
			}

			if err == nil {
				t.Fatalf("Load accepted %s as a settings document", tt.kind)
			}

			// The kind inside the refusal's own clause, not on its own. A bare
			// substring check for the kind can be satisfied by an error the
			// operating system wrote — "read <path>: is a directory" contains
			// "a directory" — and a mutant that disabled the refusal survived
			// that assertion in TestLoadUnreadableFile. Nothing but
			// refuseUnreadable writes the clause.
			if want := tt.kind + ", which cannot be read"; !strings.Contains(err.Error(), want) {
				t.Errorf("the error does not refuse the path as %s: %v", tt.kind, err)
			}

			if !strings.Contains(err.Error(), path) {
				t.Errorf("the error does not name the path %q: %v", path, err)
			}
		})
	}
}
