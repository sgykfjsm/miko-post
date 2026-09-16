//go:build darwin

package logging

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// T069 (FR-072, R-006, A-011) on the platform this tool targets.

// TestCreationTimeUsesTheBirthTimeAndNotTheModificationTime is the whole point
// of the build-tagged file: FR-072 measures the active log's age from when it
// was created, and a log appended to on every post has a modification time that
// moves forward forever. An implementation that fell back to ModTime here would
// mean the age trigger never fires on a log that is being used.
func TestCreationTimeUsesTheBirthTimeAndNotTheModificationTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jsonl")

	before := time.Now()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm)
	if err != nil {
		t.Fatalf("create the file: %v", err)
	}

	defer file.Close()

	// Long enough that the two timestamps cannot be confused for each other
	// even on a filesystem with coarse resolution.
	time.Sleep(50 * time.Millisecond)

	if _, err := file.WriteString("a record\n"); err != nil {
		t.Fatalf("append: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	created := creationTime(info)

	if created.After(info.ModTime()) {
		t.Fatalf("creation time %s is after the modification time %s", created, info.ModTime())
	}

	if !created.Before(info.ModTime()) {
		t.Fatalf("creation time equals the modification time %s, so ModTime is being used as the answer", info.ModTime())
	}

	if created.Before(before.Add(-time.Second)) || created.After(before.Add(time.Second)) {
		t.Fatalf("creation time %s is not when the file was created (%s)", created, before)
	}
}

// TestCreationTimeFallsBackWhenNoBirthTimeIsRecorded pins the other guarded
// branch. A filesystem that leaves Birthtimespec zeroed must not make every log
// look like it was created in 1970, which would rotate on every single write.
func TestCreationTimeFallsBackWhenNoBirthTimeIsRecorded(t *testing.T) {
	modified := time.Date(2026, 3, 4, 5, 6, 7, 0, time.Local)

	got := creationTime(stubInfo{modTime: modified, sys: &syscall.Stat_t{}})

	if !got.Equal(modified) {
		t.Fatalf("creationTime with a zero birth time = %s, want the modification time %s", got, modified)
	}

	if got.Year() == 1970 {
		t.Fatal("a zero birth time was taken literally, which rotates the log on every write")
	}
}
