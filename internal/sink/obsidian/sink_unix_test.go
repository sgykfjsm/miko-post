//go:build unix

package obsidian_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
)

// TestANamedPipeAtTheNotePathIsRefused covers DEC-B2's harm reached with no
// symlink at all.
//
// The symlink refusal was added because a link to /dev/null reported the post
// as delivered while storing nothing. A named pipe sitting at the note path
// does the same thing directly. Under the O_WRONLY this sink opened with
// before the separator rule, open(2) blocked until a reader attached and the
// post failed loudly on the orchestrator's timeout — an accidental guard.
// O_RDWR returns immediately, because the process holds the read end itself:
// the entry lands in the pipe buffer, close discards it, and Send returns nil.
//
// Unix-only, because syscall.Mkfifo does not exist on Windows and a FIFO is
// the cheapest object that is not a regular file. The guard it tests is not
// platform-specific: the fstat in open refuses anything that is not one.
// Following internal/logging/logger_unix_test.go, which guards its own FIFO
// case the same way.
func TestANamedPipeAtTheNotePathIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := notePath(dir, noon)

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create the FIFO in the note's place: %v", err)
	}

	// The read end is held open for the whole test, and it is what makes "and
	// nothing was stored" an assertion rather than a hope: bytes a regressed
	// build wrote would stay in the pipe buffer for this descriptor to find,
	// where the sink's own close discards them without a trace.
	reader, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open the pipe's read end: %v", err)
	}

	defer func() {
		if closeErr := syscall.Close(reader); closeErr != nil {
			t.Errorf("close the pipe's read end: %v", closeErr)
		}
	}()

	// Built here rather than in the goroutine: mustMessage can call t.Fatalf,
	// and FailNow must not be reached off the test's own goroutine.
	message := mustMessage(t, "パイプには書かない")

	sent := make(chan error, 1)

	go func() {
		sent <- obsidian.NewWithClock(settingsFor(dir), fixedClock(noon)).
			Send(context.Background(), message)
	}()

	// Send runs against a deadline instead of being called directly. An open
	// that blocks does not fail this test, it hangs it, and a timeout panic
	// naming a goroutine parked in openat takes the whole package down and
	// buries which assertion failed — the same argument
	// internal/logging/logger_unix_test.go makes. Generous on purpose: this
	// measures "returns at all".
	const deadline = 10 * time.Second

	var sendErr error

	select {
	case sendErr = <-sent:
	case <-time.After(deadline):
		// The goroutine is parked in open(2) and stays parked; there is
		// nothing to cancel. Leaking it for the rest of the run is the lesser
		// problem.
		t.Fatalf("Send did not return within %s for a named pipe at the note path", deadline)
	}

	if sendErr == nil {
		t.Fatal("Send reported the post as delivered into a named pipe")
	}

	if !errors.Is(sendErr, obsidian.ErrNoteNotRegular) {
		t.Errorf("Send: %v, want it to wrap ErrNoteNotRegular", sendErr)
	}

	// Not the symlink refusal: nothing here is a link, and the reason the user
	// reads has to be the one that is true.
	if errors.Is(sendErr, obsidian.ErrNoteIsSymlink) {
		t.Errorf("Send: %v, reported as a symbolic link", sendErr)
	}

	if !strings.Contains(sendErr.Error(), path) {
		t.Errorf("Send: %v, want it to name the note path %s", sendErr, path)
	}

	// Nothing reached the pipe, and nothing still holds it open. One
	// non-blocking read answers both: it returns no bytes and no error only
	// when the pipe is empty *and* has no writer left, and the sink's O_RDWR
	// descriptor is a writer for as long as it is open, so a refusal that
	// forgot to close would answer EAGAIN here instead.
	//
	// Worth the two extra arms because the leak is invisible from the vault:
	// FR-028 keeps one process posting for the life of a GUI window, so a note
	// path that is a FIFO or a device node would cost a descriptor per post
	// until EMFILE, at which point every sink in the process starts failing for
	// a reason that has nothing to do with this one.
	buffered := make([]byte, 256)

	switch n, readErr := syscall.Read(reader, buffered); {
	case n > 0:
		t.Errorf("the pipe received %q; the entry was written into it and discarded (read err %v)",
			buffered[:n], readErr)
	case errors.Is(readErr, syscall.EAGAIN):
		t.Error("the pipe still has a writer attached after the refusal; the sink did not release its descriptor")
	case readErr != nil:
		t.Fatalf("read the pipe after the refusal: %v", readErr)
	}

	// And the pipe is still the pipe: nothing was unlinked or replaced.
	info, statErr := os.Lstat(path)
	if statErr != nil {
		t.Fatalf("lstat the note path after the refusal: %v", statErr)
	}

	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Errorf("%s is now mode %s; the sink replaced what was at the note path", path, info.Mode())
	}
}
