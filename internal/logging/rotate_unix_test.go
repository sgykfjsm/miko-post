//go:build unix

package logging

import (
	"fmt"
	"os"
	"testing"
)

// openFileCount reports how many descriptors this process holds, which unix
// exposes as a directory.
func openFileCount(t *testing.T) int {
	t.Helper()

	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(dir)
		if err == nil {
			return len(entries)
		}
	}

	t.Skip("this platform does not expose the process's open descriptors")

	return 0
}

// TestRotateReleasesTheArchivedHandle pins that each rotation closes the file
// it archived.
//
// Nothing else can see this. The records are correct either way, every other
// test in this file passes, and the only symptom is a descriptor per rotation —
// which on a log rotating at its size threshold under a runaway writer is how a
// process reaches its descriptor limit and starts failing to open anything at
// all, including the sinks. It is exactly the class of defect a diff review
// reads straight past, so it gets the one test that can fail on it.
func TestRotateReleasesTheArchivedHandle(t *testing.T) {
	_, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 1})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	// One write before the baseline so the active handle is already counted.
	write(t, w, "warm-up")

	before := openFileCount(t)

	const rotations = 50

	for i := range rotations {
		write(t, w, fmt.Sprintf("record-%d", i))
	}

	after := openFileCount(t)

	// A handful of slack for the runtime's own descriptors; a leak would be
	// fifty.
	const slack = 5

	if after-before > slack {
		t.Fatalf("%d rotations leaked descriptors: %d open before, %d after", rotations, before, after)
	}
}
