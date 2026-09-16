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

// TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime carries
// the test above through open(), which is the only place the answer is used.
//
// creationTime can be entirely correct and rotate.go still throw its answer
// away. Replace `w.createdAt = creationTime(info)` with `info.ModTime()` and
// every portable test in this package stays green, because everywhere but
// darwin those two expressions are the same function — so this build is the
// only place the substitution can be caught. It is worth catching: it silently
// reverts the whole reason A-011 reaches for Birthtimespec. A log appended to
// on every post has a modification time that moves forward forever, so a
// createdAt taken from ModTime means a user who posts more often than
// rotate_after_days never sees the age trigger fire at all.
//
// The clock is placed at the file's modification time and the threshold is set
// to the gap between the two timestamps. Measured from the birth time the file
// is exactly at the threshold and rotates; measured from the modification time
// it is zero seconds old and does not. Nothing depends on how wide the gap is,
// only that it is not zero.
func TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime(t *testing.T) {
	dir, path := logDir(t)

	if err := os.WriteFile(path, []byte("from an earlier run\n"), logFilePerm); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	// Long enough that the two timestamps cannot be confused for each other
	// even on a filesystem with coarse resolution. os.Chtimes cannot open the
	// gap instead: moving the modification time backwards carries the birth
	// time back with it on APFS, and moving it forwards would date the file in
	// the future.
	time.Sleep(50 * time.Millisecond)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, logFilePerm)
	if err != nil {
		t.Fatalf("reopen the seeded log: %v", err)
	}

	if _, err := file.WriteString("and one more\n"); err != nil {
		t.Fatalf("append to the seeded log: %v", err)
	}

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat the seeded log: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close the seeded log: %v", err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("FileInfo.Sys is %T, not *syscall.Stat_t", info.Sys())
	}

	sec, nsec := stat.Birthtimespec.Unix()
	if sec == 0 && nsec == 0 {
		// A volume with no birth time — SMB, NFS, exFAT — makes creationTime
		// fall back to ModTime by design, so there is nothing here to tell
		// apart. Skipping is honest; asserting would pin the fallback as if it
		// were the rule.
		t.Skip("this filesystem records no birth time")
	}

	birth := time.Unix(sec, nsec)

	// A volume can record a birth time and still be unable to express the gap
	// this test needs: FAT32 stamps modification times to two seconds and HFS+
	// to one, so the 50ms sleep above lands inside a single tick and both
	// timestamps come back equal. There is then nothing to tell apart, and the
	// environment — not internal/logging — is what is unsupported. A t.Fatalf
	// here would be a false RED that reads as a rotation bug in product code
	// that is working. The skip above covers only the other unsupported case, a
	// volume with no birth time at all.
	//
	// This weakens nothing: on any volume that CAN express the gap, everything
	// below still runs and open() must date the pre-existing log from its birth
	// time. APFS, where this suite runs, resolves to the nanosecond.
	gap := info.ModTime().Sub(birth)
	if gap <= 0 {
		t.Skipf(
			"this filesystem cannot express a birth/modification gap: birth %s, modification %s, gap %s",
			birth, info.ModTime(), gap,
		)
	}

	clock := &fixedClock{now: info.ModTime()}

	w, err := openRotating(path, rotationOptions{maxAge: gap, now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "after the restart\n")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one: the log is %s old measured from its birth time and 0s old measured from its modification time", rotated, gap)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "from an earlier run\nand one more\n" {
		t.Fatalf("archive = %q, want the seeded log intact", got)
	}
}
