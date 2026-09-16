package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// T065 (FR-072 - FR-074). An internal test, following queue_test.go, because
// the subject is the rotating writer's own decision points: which of the two
// conditions fired, what name was claimed, and what survives each failure. The
// exported surface reaches those only through Open, which is exercised at the
// bottom of this file.

// fixedClock is a settable clock. The age condition is otherwise untestable
// without sleeping for days, and the rotated-name suffix is otherwise
// unpredictable.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fixedClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = t
}

// flakyStat fails the nth call to stat the freshly opened active file, which is
// how the branches that cannot be reached through a real filesystem — a handle
// whose size and creation time cannot be read — are exercised.
//
// failFrom, when positive, fails that call and every one after it. A hiccup and
// a persistent failure are different tests: the first recovers on the next
// write, while the second is the one that reopens the file for every record, so
// it is the only way to see what the refusal path forgets to release.
type flakyStat struct {
	mu          sync.Mutex
	calls       int
	fail        map[int]error
	failFrom    int
	failFromErr error
}

func (f *flakyStat) stat(file *os.File) (os.FileInfo, error) {
	f.mu.Lock()
	f.calls++
	err := f.fail[f.calls]

	if err == nil && f.failFrom > 0 && f.calls >= f.failFrom {
		err = f.failFromErr
	}

	f.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return file.Stat()
}

// logDir returns a temporary directory and the active log path inside it.
func logDir(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()

	return dir, filepath.Join(dir, "app.jsonl")
}

// archives returns every file in dir except the active log, sorted by name.
func archives(t *testing.T, dir, active string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the log directory: %v", err)
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if full := filepath.Join(dir, entry.Name()); full != active {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)

	return names
}

func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

// sizeOf reports the current size of path, or 0 if it cannot be measured.
//
// It takes no *testing.T on purpose: its caller runs on a spawned goroutine,
// where t.Fatalf is illegal, and every use is a "has anything landed yet?"
// probe where a missing file and an empty one mean the same thing. A rotation
// racing the probe answers 0 for the fresh active file, which only delays the
// answer to the next record.
func sizeOf(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}

	return info.Size()
}

func write(t *testing.T, w *rotatingWriter, s string) {
	t.Helper()

	n, err := w.Write([]byte(s))
	if err != nil {
		t.Fatalf("write %q: %v", s, err)
	}

	if n != len(s) {
		t.Fatalf("write %q: wrote %d of %d bytes", s, n, len(s))
	}
}

// TestRotateSizeTriggerFiresAtTheThresholdAndNotBefore pins FR-072's size
// condition and the fact that it is evaluated *before* the write rather than
// after it: the record that takes the file to the threshold still belongs to
// the file it filled, and the next one starts the new active log.
func TestRotateSizeTriggerFiresAtTheThresholdAndNotBefore(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 10})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	// Nine bytes: under the threshold, so nothing rotates.
	write(t, w, "123456789")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("rotated below the threshold: %v", got)
	}

	// One more byte reaches exactly 10. The check runs before this write, and
	// before it the size was 9, so this record still lands in the active file.
	write(t, w, "A")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("rotated on the write that reached the threshold, not before the next: %v", got)
	}

	if got := read(t, path); got != "123456789A" {
		t.Fatalf("active log = %q, want %q", got, "123456789A")
	}

	// Now size (10) >= maxSize (10) before the write, so this one rotates.
	write(t, w, "B")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "123456789A" {
		t.Fatalf("archive = %q, want the records written before rotation", got)
	}

	if got := read(t, path); got != "B" {
		t.Fatalf("new active log = %q, want %q", got, "B")
	}
}

// TestRotateSizeCountsWhatWasAlreadyOnDisk pins FR-072's "captured when the
// file is opened": a process restarting onto a log that is already at the
// threshold must rotate on its first write, not after writing a second
// threshold's worth. It also pins that the earlier run's records survive, which
// they would not if the file were opened truncating (FR-074).
func TestRotateSizeCountsWhatWasAlreadyOnDisk(t *testing.T) {
	dir, path := logDir(t)

	if err := os.WriteFile(path, []byte("from an earlier run\n"), logFilePerm); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	w, err := openRotating(path, rotationOptions{maxSize: 10})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "new\n")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "from an earlier run\n" {
		t.Fatalf("archive = %q, want the earlier run's records intact", got)
	}

	if got := read(t, path); got != "new\n" {
		t.Fatalf("new active log = %q", got)
	}
}

// TestRotateAgeTriggerFiresIndependentlyOfSize pins FR-072's second condition
// and where its boundary actually falls. The file is one byte long, so only age
// can explain the rotation.
//
// The threshold is rotateAge(7) and the boundaries below are written in literal
// hours, deliberately. An assertion stated as a multiple of dayDuration puts the
// constant under test on both sides of the comparison and agrees with any value
// of it: dayDuration could become 23 hours — turning "rotate_after_days = 7"
// into a rotation every 6 days 23 hours — with every such assertion still
// passing. Seven days is 168 hours here or the test is wrong.
func TestRotateAgeTriggerFiresIndependentlyOfSize(t *testing.T) {
	dir, path := logDir(t)

	clock := &fixedClock{now: time.Now()}

	w, err := openRotating(path, rotationOptions{maxAge: rotateAge(7), now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("rotated before the age threshold: %v", got)
	}

	// One nanosecond under seven literal days: still the same file. This is the
	// half a shortened day fails, because a short day makes the file expire
	// here.
	clock.set(w.createdAt.Add(7*24*time.Hour - time.Nanosecond))

	write(t, w, "b")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("rotated a nanosecond before seven days: %v", got)
	}

	// Exactly at the threshold: the comparison is >=, so this rotates. This is
	// the half a lengthened day fails.
	clock.set(w.createdAt.Add(7 * 24 * time.Hour))

	write(t, w, "c")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "ab" {
		t.Fatalf("archive = %q, want %q", got, "ab")
	}

	if got := read(t, path); got != "c" {
		t.Fatalf("new active log = %q, want %q", got, "c")
	}
}

// backdatedInfo is the real file's FileInfo with a creation time the filesystem
// could not plausibly have produced.
//
// Sys returns nil so that darwin takes creationTime's documented fallback to
// ModTime rather than reading a Birthtimespec this type cannot forge; the test
// below therefore measures the same thing on every platform.
type backdatedInfo struct {
	os.FileInfo

	modTime time.Time
}

func (b backdatedInfo) ModTime() time.Time { return b.modTime }
func (b backdatedInfo) Sys() any           { return nil }

// TestRotateCannotFireRepeatedlyOnAFileItJustCreated pins that a rotation
// clears its own trigger.
//
// The age condition subtracts two clocks that nothing keeps together: the
// process clock on one side and whatever the filesystem recorded on the other.
// Put the first far enough ahead of the second — a log directory on a network
// mount whose server clock is days behind, a darwin volume reporting a non-zero
// but nonsensical birth time — and a file created a microsecond ago is already
// expired, so every write rotates, the log becomes one file per record, and the
// first archive is zero bytes. Nothing about the size trigger can do this,
// because a rotation always empties the file.
//
// Here the stat seam reports every freshly opened file as thirty days old
// against a seven-day threshold, which is the worst case rather than a likely
// one. One rotation is correct: the seeded records are genuinely there and
// genuinely old. A second would be the unbounded case.
func TestRotateCannotFireRepeatedlyOnAFileItJustCreated(t *testing.T) {
	dir, path := logDir(t)

	if err := os.WriteFile(path, []byte("from an earlier run\n"), logFilePerm); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	clock := &fixedClock{now: time.Now()}

	backdate := func(file *os.File) (os.FileInfo, error) {
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}

		return backdatedInfo{FileInfo: info, modTime: clock.Now().Add(-30 * 24 * time.Hour)}, nil
	}

	w, err := openRotating(path, rotationOptions{maxAge: rotateAge(7), now: clock.Now, stat: backdate})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	const writes = 5

	for i := range writes {
		write(t, w, fmt.Sprintf("record-%d\n", i))
	}

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("%d writes produced %d archives (%v); a rotation did not clear its own trigger", writes, len(rotated), rotated)
	}

	// The one archive holds the records that were actually old. An archive of
	// zero bytes is the signature of the defect: a file rotated before anything
	// was written to it.
	if got := read(t, filepath.Join(dir, rotated[0])); got != "from an earlier run\n" {
		t.Fatalf("archive = %q, want the seeded records", got)
	}

	// And every record written afterwards is in the active log, in order.
	var want strings.Builder

	for i := range writes {
		fmt.Fprintf(&want, "record-%d\n", i)
	}

	if got := read(t, path); got != want.String() {
		t.Fatalf("active log = %q, want %q", got, want.String())
	}
}

// TestReopeningANonEmptyLogDatesItFromTheFileAndNotTheClock pins the half of
// FR-072's age condition that nothing else in this package can fail on: the
// "too old" direction.
//
// Every other age assertion here either uses an empty file — whose createdAt
// open() deliberately overwrites with now(), so the filesystem's answer is not
// observable through it at all — or seeds a genuinely old file and asserts that
// it rotates, which an arbitrarily early value satisfies just as well. Without
// this test `w.createdAt = creationTime(info)` can be replaced by the zero time
// or by the Unix epoch with the whole package still green, and the cost of that
// is not theoretical: mp is a short-lived CLI, so every run reopens the log, and
// a log dated in 1970 is archived on the first write of every run. The user
// ends up with one file per post and an active log that is always empty.
//
// Both halves are load-bearing. The first pins that open() does not date a
// pre-existing log *earlier* than the file says; the second pins that it does
// not date it *later*, which is what a createdAt pinned to now() for every file
// would do — the age trigger would then never fire at all. The reference for
// both is the file's own recorded timestamp rather than w.createdAt, because an
// assertion phrased in terms of the value under test agrees with any value of
// it.
//
// What is not observable here is the difference between a real creation time
// and ModTime: on every platform but darwin creationTime *is* ModTime, so no
// portable test can tell the two apart. That half is
// TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime in
// birthtime_darwin_test.go.
func TestReopeningANonEmptyLogDatesItFromTheFileAndNotTheClock(t *testing.T) {
	dir, path := logDir(t)

	// A log left behind by an earlier run, created a moment ago. Not empty, so
	// open() keeps the filesystem's answer instead of the clock's.
	if err := os.WriteFile(path, []byte("from a run a moment ago\n"), logFilePerm); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	seeded, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the seeded log: %v", err)
	}

	clock := &fixedClock{now: time.Now()}

	w, err := openRotating(path, rotationOptions{maxAge: rotateAge(7), now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "after the restart\n")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("a log created moments ago was archived on the first write after a restart: %v", got)
	}

	// Seven literal days after the file was written, and not after the writer
	// was opened: the creation time is never later than the modification time,
	// so this instant is at or past the threshold for the real answer and short
	// of it for anything dated from the open.
	clock.set(seeded.ModTime().Add(7 * 24 * time.Hour))

	write(t, w, "a week later\n")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one once the seeded log is seven days old", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "from a run a moment ago\nafter the restart\n" {
		t.Fatalf("archive = %q, want the seeded log and the record that followed it", got)
	}

	if got := read(t, path); got != "a week later\n" {
		t.Fatalf("new active log = %q, want only the record written after the rotation", got)
	}
}

// TestRotateEvaluatesBothConditionsBeforeEveryWrite is the mutation guard for
// FR-072's "both". Each subtest disables one condition entirely, so a writer
// that consulted only the other would leave one of them unrotated.
func TestRotateEvaluatesBothConditionsBeforeEveryWrite(t *testing.T) {
	cases := map[string]struct {
		opts    func(clock *fixedClock) rotationOptions
		advance bool
	}{
		"size alone": {
			opts: func(clock *fixedClock) rotationOptions {
				return rotationOptions{maxSize: 1, now: clock.Now}
			},
		},
		"age alone": {
			opts: func(clock *fixedClock) rotationOptions {
				return rotationOptions{maxAge: time.Hour, now: clock.Now}
			},
			advance: true,
		},
		"both set": {
			opts: func(clock *fixedClock) rotationOptions {
				return rotationOptions{maxSize: 1, maxAge: time.Hour, now: clock.Now}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, path := logDir(t)

			clock := &fixedClock{now: time.Now()}

			w, err := openRotating(path, tc.opts(clock))
			if err != nil {
				t.Fatalf("open the rotating writer: %v", err)
			}

			t.Cleanup(func() { _ = w.Close() })

			write(t, w, "x")

			if tc.advance {
				clock.set(clock.Now().Add(2 * time.Hour))
			}

			write(t, w, "y")

			if got := archives(t, dir, path); len(got) != 1 {
				t.Fatalf("archives = %v, want exactly one", got)
			}
		})
	}
}

// TestRotateDisabledThresholdsNeverRotate pins that a threshold of zero or less
// is inert rather than matching everything — the difference between a logger
// that does not rotate and one that turns every record into its own file.
func TestRotateDisabledThresholdsNeverRotate(t *testing.T) {
	for name, opts := range map[string]rotationOptions{
		"both unset": {},
		"negative":   {maxSize: -1, maxAge: -time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			dir, path := logDir(t)

			opts := opts
			opts.now = func() time.Time { return time.Now().Add(100 * 365 * dayDuration) }

			w, err := openRotating(path, opts)
			if err != nil {
				t.Fatalf("open the rotating writer: %v", err)
			}

			t.Cleanup(func() { _ = w.Close() })

			for range 20 {
				write(t, w, "record\n")
			}

			if got := archives(t, dir, path); len(got) != 0 {
				t.Fatalf("rotated with the trigger disabled: %v", got)
			}
		})
	}
}

// TestRotateSuffixIsTheLocalTimestampFormat pins FR-073's exact suffix.
func TestRotateSuffixIsTheLocalTimestampFormat(t *testing.T) {
	dir, path := logDir(t)

	// A time with no ambiguous digits, in local time, so a UTC implementation
	// fails everywhere except a UTC machine — and the parse below catches the
	// shape anywhere.
	stamp := time.Date(2026, 8, 27, 11, 42, 3, 0, time.Local)
	clock := &fixedClock{now: stamp}

	w, err := openRotating(path, rotationOptions{maxSize: 1, now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")
	write(t, w, "b")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	want := "app.jsonl.20260827114203"
	if rotated[0] != want {
		t.Fatalf("archive = %q, want %q", rotated[0], want)
	}

	suffix := strings.TrimPrefix(rotated[0], "app.jsonl.")
	if !regexp.MustCompile(`^[0-9]{14}$`).MatchString(suffix) {
		t.Fatalf("suffix %q is not YYYYMMDDhhmmss", suffix)
	}

	parsed, err := time.ParseInLocation(rotationSuffixLayout, suffix, time.Local)
	if err != nil {
		t.Fatalf("parse the suffix %q: %v", suffix, err)
	}

	if !parsed.Equal(stamp) {
		t.Fatalf("suffix decodes to %s, want the local rotation time %s", parsed, stamp)
	}
}

// TestRotateNeverDeletesOrOverwritesARotatedFile is FR-074 and constitution
// principle VI. Every rotation is driven onto the same clock second, so the
// collision rule is the only thing standing between eight rotations and one
// surviving file.
func TestRotateNeverDeletesOrOverwritesARotatedFile(t *testing.T) {
	dir, path := logDir(t)

	clock := &fixedClock{now: time.Date(2026, 8, 27, 11, 42, 3, 0, time.Local)}

	w, err := openRotating(path, rotationOptions{maxSize: 1, now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	const records = 8

	for i := range records {
		write(t, w, fmt.Sprintf("record-%d", i))
	}

	// records writes produce records-1 rotations: the first write finds an
	// empty file.
	rotated := archives(t, dir, path)
	if len(rotated) != records-1 {
		t.Fatalf("archives = %v, want %d of them", rotated, records-1)
	}

	// Every record is still readable, exactly once, in order.
	for i, name := range rotated {
		want := fmt.Sprintf("record-%d", i)
		if got := read(t, filepath.Join(dir, name)); got != want {
			t.Fatalf("archive %s = %q, want %q", name, got, want)
		}
	}

	if got := read(t, path); got != fmt.Sprintf("record-%d", records-1) {
		t.Fatalf("active log = %q, want the last record", got)
	}

	// And the names are the documented collision sequence.
	want := []string{"app.jsonl.20260827114203"}
	for i := 1; i < records-1; i++ {
		want = append(want, fmt.Sprintf("app.jsonl.20260827114203-%d", i))
	}

	sort.Strings(want)

	if strings.Join(rotated, ",") != strings.Join(want, ",") {
		t.Fatalf("archives = %v, want %v", rotated, want)
	}
}

// TestRotateStepsOverAFileItDidNotCreate pins that the collision rule protects
// anything already at the target name, not only this writer's own archives.
func TestRotateStepsOverAFileItDidNotCreate(t *testing.T) {
	_, path := logDir(t)

	clock := &fixedClock{now: time.Date(2026, 8, 27, 11, 42, 3, 0, time.Local)}
	occupied := path + ".20260827114203"

	if err := os.WriteFile(occupied, []byte("not ours"), logFilePerm); err != nil {
		t.Fatalf("seed the occupied name: %v", err)
	}

	w, err := openRotating(path, rotationOptions{maxSize: 1, now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")
	write(t, w, "b")

	if got := read(t, occupied); got != "not ours" {
		t.Fatalf("occupied name = %q, want it untouched", got)
	}

	if got := read(t, occupied+"-1"); got != "a" {
		t.Fatalf("archive = %q, want the rotated records at the -1 name", got)
	}
}

// TestClaimNameReservesTheNameAtomically pins the reason the collision rule is
// an O_EXCL create rather than a stat: two racing rotations must not be handed
// the same name. A stat-then-rename implementation passes every other test in
// this file and loses a log here.
func TestClaimNameReservesTheNameAtomically(t *testing.T) {
	_, path := logDir(t)

	clock := &fixedClock{now: time.Date(2026, 8, 27, 11, 42, 3, 0, time.Local)}
	w := &rotatingWriter{path: path, now: clock.Now}

	const claims = 64

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		names  = make(map[string]int, claims)
		failed []error
	)

	for range claims {
		wg.Add(1)

		go func() {
			defer wg.Done()

			name, err := w.claimName()

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failed = append(failed, err)

				return
			}

			names[name]++
		}()
	}

	wg.Wait()

	if len(failed) != 0 {
		t.Fatalf("claims failed: %v", failed)
	}

	if len(names) != claims {
		t.Fatalf("%d concurrent claims produced %d distinct names; a name was handed out twice", claims, len(names))
	}
}

// TestClaimNameIsBounded pins that the search for a free name always ends.
// Spinning here would hang a log write, and FR-076's first duty is that
// diagnostics never block a post — so both ways out of the loop are pinned.
func TestClaimNameIsBounded(t *testing.T) {
	t.Run("the directory refuses every create", func(t *testing.T) {
		dir, path := logDir(t)

		// A read-only directory makes every O_EXCL create fail with EACCES
		// rather than EEXIST, which is the other exit from the loop.
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("make the directory read-only: %v", err)
		}

		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		w := &rotatingWriter{path: path, now: time.Now}

		if _, err := w.claimName(); err == nil {
			t.Fatal("claimName succeeded in a read-only directory")
		} else if !strings.Contains(err.Error(), "claim a rotated log name") {
			t.Fatalf("claimName error = %v, want it to name what failed", err)
		}
	})

	t.Run("every candidate name is taken", func(t *testing.T) {
		_, path := logDir(t)

		stamp := time.Date(2026, 8, 27, 11, 42, 3, 0, time.Local)
		base := path + ".20260827114203"

		// The unnumbered name plus every numbered variant the rule will try.
		if err := os.WriteFile(base, nil, logFilePerm); err != nil {
			t.Fatalf("occupy %s: %v", base, err)
		}

		for i := 1; i <= maxCollisionAttempts; i++ {
			name := fmt.Sprintf("%s-%d", base, i)
			if err := os.WriteFile(name, nil, logFilePerm); err != nil {
				t.Fatalf("occupy %s: %v", name, err)
			}
		}

		w := &rotatingWriter{path: path, now: func() time.Time { return stamp }}

		_, err := w.claimName()
		if err == nil {
			t.Fatal("claimName returned a name that was already taken")
		}

		if !strings.Contains(err.Error(), "are all taken") {
			t.Fatalf("claimName error = %v, want it to say the names ran out", err)
		}
	})
}

// TestRotateKeepsLoggingWhenTheNameCannotBeClaimed pins the first failure path:
// nothing has moved, so the record still lands in the active file and the
// caller learns why through the returned error (FR-076).
func TestRotateKeepsLoggingWhenTheNameCannotBeClaimed(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 1})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("make the directory read-only: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	n, err := w.Write([]byte("b"))
	if err == nil {
		t.Fatal("a failed rotation reported no error, so no warning would be raised")
	}

	if n != 1 {
		t.Fatalf("wrote %d bytes, want the record written despite the failed rotation", n)
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore the directory: %v", err)
	}

	if got := read(t, path); got != "ab" {
		t.Fatalf("active log = %q, want both records", got)
	}

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("a failed rotation left files behind: %v", got)
	}
}

// TestRotateLeavesNoPlaceholderWhenTheRenameFails pins the second failure path.
// The active log is unlinked underneath the writer, which is what a user
// clearing out a log directory does, so the rename has a claimed name and
// nothing to move onto it.
func TestRotateLeavesNoPlaceholderWhenTheRenameFails(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 1})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the active log: %v", err)
	}

	n, err := w.Write([]byte("b"))
	if err == nil {
		t.Fatal("a failed rename reported no error")
	}

	if !strings.Contains(err.Error(), "rotate the log to") {
		t.Fatalf("error = %v, want it to name the rotation it could not complete", err)
	}

	if n != 1 {
		t.Fatalf("wrote %d bytes, want the record still written", n)
	}

	// The claimed placeholder is this writer's own empty file and must not be
	// left behind masquerading as an archive.
	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("a failed rename left %v behind", got)
	}
}

// TestRotateKeepsTheArchiveWhenTheReplacementCannotBeOpened pins the third
// failure path, the only one that loses records: the rename succeeded, so
// everything written so far is safe under the archived name, and the writer
// reports the failure instead of pretending to log.
func TestRotateKeepsTheArchiveWhenTheReplacementCannotBeOpened(t *testing.T) {
	dir, path := logDir(t)

	sentinel := errors.New("no stat for you")
	// Call 1 is the initial open. Calls 2 and 3 are the reopen inside rotate
	// and the retry Write makes straight after it.
	flaky := &flakyStat{fail: map[int]error{2: sentinel, 3: sentinel}}

	w, err := openRotating(path, rotationOptions{maxSize: 1, stat: flaky.stat})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")

	n, err := w.Write([]byte("b"))
	if err == nil {
		t.Fatal("a failed reopen reported no error")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want it to carry the underlying cause", err)
	}

	if n != 0 {
		t.Fatalf("wrote %d bytes to a writer with no active file", n)
	}

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want the rotated records kept", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "a" {
		t.Fatalf("archive = %q, want %q", got, "a")
	}

	// And the writer recovers rather than staying dead for the life of the
	// process: the next write reopens the active log.
	write(t, w, "c")

	if got := read(t, path); got != "c" {
		t.Fatalf("active log after recovery = %q, want %q", got, "c")
	}
}

// closeUnderneath closes the writer's active handle behind its back, so that
// the writer's own close of it fails.
//
// This is the only way to reach the close-failure paths from a test, and it is
// not a contrived failure. (*os.File).Close reports the deferred write error a
// buffered filesystem discovered after the write returned — ENOSPC, EIO, a
// disconnected network mount — which means the records the writer already
// counted as logged may never have reached the platter. That is precisely the
// case FR-076 owes the user its one warning for, so it must not be swallowed.
// os.ErrClosed is the portable stand-in: a second close reports it everywhere,
// where filling a disk inside a unit test does not.
func closeUnderneath(t *testing.T, w *rotatingWriter) {
	t.Helper()

	if w.file == nil {
		t.Fatal("the writer has no active file to close")
	}

	if err := w.file.Close(); err != nil {
		t.Fatalf("close the active handle underneath the writer: %v", err)
	}
}

// TestRotateReportsAFailedCloseOfTheArchivedFile pins that the close of the
// file a rotation just archived is reported and not dropped. Everything else
// about the rotation succeeded — the archive is on disk and the new active log
// is open — so a discarded close error is a rotation that looks perfect while
// the archived records may be incomplete.
func TestRotateReportsAFailedCloseOfTheArchivedFile(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 1})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")
	closeUnderneath(t, w)

	n, err := w.Write([]byte("b"))
	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write = %v, want it to carry the archived file's close failure", err)
	}

	// And the failure is reported *alongside* the work that succeeded, not
	// instead of it: the record still lands and the archive is still there.
	if n != 1 {
		t.Fatalf("wrote %d bytes, want the record written despite the close failure", n)
	}

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	if got := read(t, path); got != "b" {
		t.Fatalf("new active log = %q, want %q", got, "b")
	}
}

// TestRotateReportsEveryReasonAWriteFailed pins that a Write which failed for
// more than one reason reports all of them. Three things go wrong here at once:
// the archived file's close fails, the reopen inside the rotation fails, and
// the retry the Write makes afterwards fails too.
//
// Each is a different repair. A caller told only that the log could not be
// reopened will look at the directory; one told only that a close failed will
// look at the disk. FR-076 spends a single warning, so that warning has to
// carry every reason there was.
func TestRotateReportsEveryReasonAWriteFailed(t *testing.T) {
	dir, path := logDir(t)

	sentinel := errors.New("no stat for you")
	// Call 1 is the initial open. Every call after it fails, which covers both
	// the reopen inside rotate and the retry Write makes straight after it.
	flaky := &flakyStat{failFrom: 2, failFromErr: sentinel}

	w, err := openRotating(path, rotationOptions{maxSize: 1, stat: flaky.stat})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")
	closeUnderneath(t, w)

	n, err := w.Write([]byte("b"))
	if err == nil {
		t.Fatal("a rotation that failed three ways reported no error")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want it to carry the reason the log could not be reopened", err)
	}

	if !errors.Is(err, os.ErrClosed) {
		t.Fatalf("error = %v, want it to carry the archived file's close failure as well", err)
	}

	if n != 0 {
		t.Fatalf("wrote %d bytes to a writer with no active file", n)
	}

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want the rotated records kept", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "a" {
		t.Fatalf("archive = %q, want %q", got, "a")
	}
}

// TestCloseFailureReachesTheFrontDoor pins the last few feet of the same path:
// a close that reports a deferred write error has to become something the user
// sees, through both surfaces a front door consults. Logger.Close is what a
// caller checks on the way out, and Degraded is FR-076's one warning.
func TestCloseFailureReachesTheFrontDoor(t *testing.T) {
	// rotatingWriterOf reaches the writer Open built, which is the only way to
	// arrange a failing close on a Logger's own handle.
	rotatingWriterOf := func(t *testing.T, logger *Logger) *rotatingWriter {
		t.Helper()

		w, ok := logger.writer.owned.(*rotatingWriter)
		if !ok {
			t.Fatalf("the logger owns a %T, not a rotating writer", logger.writer.owned)
		}

		return w
	}

	t.Run("Logger.Close reports it", func(t *testing.T) {
		_, path := logDir(t)

		logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1})

		logger.Post("post-1").Info(EventMessageReceived)

		closeUnderneath(t, rotatingWriterOf(t, logger))

		if err := logger.Close(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("Logger.Close = %v, want the failed close reported", err)
		}
	})

	t.Run("a rotation's failed close becomes the one warning", func(t *testing.T) {
		dir, path := logDir(t)

		// Already past the 1 MiB threshold, so the first record rotates.
		if err := os.WriteFile(path, make([]byte, 2<<20), logFilePerm); err != nil {
			t.Fatalf("seed an oversized log: %v", err)
		}

		logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1})

		t.Cleanup(func() { _ = logger.Close() })

		closeUnderneath(t, rotatingWriterOf(t, logger))

		logger.Post("post-1").Info(EventMessageReceived)

		degraded := logger.Degraded()
		if degraded == nil {
			t.Fatal("a rotation whose close failed raised no warning")
		}

		if !errors.Is(degraded.Err, os.ErrClosed) {
			t.Fatalf("warning = %q, want it to carry the close failure", degraded.Warning())
		}

		if degraded.Path != path {
			t.Fatalf("warning names %q, want the log path %q", degraded.Path, path)
		}

		// The rotation itself still succeeded, so the record is on disk and the
		// archive is intact: this is a warning about durability, not a loss.
		if got := archives(t, dir, path); len(got) != 1 {
			t.Fatalf("archives = %v, want exactly one", got)
		}

		if !strings.Contains(read(t, path), `"event":"message_received"`) {
			t.Fatal("the record was dropped when the archived file's close failed")
		}
	})
}

// TestOpenRotatingRefusesAFileItCannotMeasure pins that a handle whose size and
// creation time are unreadable degrades rather than becoming a log that grows
// without bound because neither condition can ever fire.
func TestOpenRotatingRefusesAFileItCannotMeasure(t *testing.T) {
	_, path := logDir(t)

	sentinel := errors.New("no stat for you")
	flaky := &flakyStat{fail: map[int]error{1: sentinel}}

	w, err := openRotating(path, rotationOptions{maxSize: 1, stat: flaky.stat})
	if err == nil {
		_ = w.Close()

		t.Fatal("openRotating accepted a file it could not measure")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want it to carry the underlying cause", err)
	}

	if !strings.Contains(err.Error(), "measure the log file") {
		t.Fatalf("error = %v, want it to say what failed", err)
	}
}

// TestOpenRotatingRefusesANonRegularPath pins that rotation inherits the
// package's refusal rather than reimplementing the open: openRotating goes
// through openLogFile, so usableAsLog's check guards the rotating writer's
// first handle and every one it opens after a rotation, and the error names
// what is actually at the path.
//
// A directory and not a FIFO, deliberately. What is under test is which code
// path openRotating takes, and a directory is the one non-regular file every
// platform can create in a test. The kind that motivated the guard — a named
// pipe, where os.OpenFile blocks inside open(2) until a reader attaches and the
// post never runs at all — is covered by mode in
// TestUsableAsLogAllowsWhatCanBeAppendedTo and end to end against a deadline by
// TestOpenDoesNotBlockOnANonRegularFile in logger_unix_test.go.
func TestOpenRotatingRefusesANonRegularPath(t *testing.T) {
	dir, _ := logDir(t)

	path := filepath.Join(dir, "as-a-directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create the directory: %v", err)
	}

	w, err := openRotating(path, rotationOptions{})
	if err == nil {
		_ = w.Close()

		t.Fatal("openRotating accepted a directory as the log path")
	}

	if !strings.Contains(err.Error(), "a directory") {
		t.Fatalf("error = %v, want it to name the kind of file", err)
	}
}

// TestRotatingWriterCloseIsIdempotentAndFinal pins that Close can be called
// twice and that a straggling write afterwards is refused rather than silently
// resurrecting the log.
func TestRotatingWriterCloseIsIdempotentAndFinal(t *testing.T) {
	_, path := logDir(t)

	w, err := openRotating(path, rotationOptions{})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	write(t, w, "a")

	if err := w.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if _, err := w.Write([]byte("b")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("write after close: %v, want os.ErrClosed", err)
	}

	if got := read(t, path); got != "a" {
		t.Fatalf("log = %q, want the write after close discarded", got)
	}
}

// TestRotatingWriterIsSafeUnderConcurrentWrites is the race-detector guard.
// safeWriter serialises production writes, but a type whose safety depends on
// its caller is a trap for the next one.
func TestRotatingWriterIsSafeUnderConcurrentWrites(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 64})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	const writers = 8

	var wg sync.WaitGroup

	for i := range writers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := range 20 {
				if _, err := w.Write([]byte(fmt.Sprintf("writer-%d-record-%d\n", i, j))); err != nil {
					t.Errorf("concurrent write: %v", err)

					return
				}
			}
		}()
	}

	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Nothing is lost: every record is somewhere in the directory exactly once.
	var total int

	for _, name := range append(archives(t, dir, path), filepath.Base(path)) {
		total += strings.Count(read(t, filepath.Join(dir, name)), "\n")
	}

	if want := writers * 20; total != want {
		t.Fatalf("recovered %d records across the log and its archives, want %d", total, want)
	}
}

// TestRotatingWriterCloseRacingAWriteIsWrittenOrRefused is the other half of
// the race coverage, and the half that is the production arrangement rather
// than a hypothetical one: recordQueue's worker calls safeWriter.close, which
// closes this writer, while a PostLogger goroutine may still be emitting.
// Close is idempotent and final when nothing else is running
// (TestRotatingWriterCloseIsIdempotentAndFinal) and writes are safe against
// each other when Close is not (the test above); neither says what happens when
// the two overlap.
//
// The property is that a write racing Close lands or is refused, and nothing in
// between: no panic, no write against a closed handle, and no record counted as
// written that is not on disk. The accounting at the end is what makes that
// checkable — a refused write must contribute nothing, and an accepted one must
// contribute exactly one line.
func TestRotatingWriterCloseRacingAWriteIsWrittenOrRefused(t *testing.T) {
	dir, path := logDir(t)

	// A small threshold so rotation is running concurrently with the close too:
	// rotate is the part that swaps the handle, and it is where an unlocked
	// close would be caught.
	w, err := openRotating(path, rotationOptions{maxSize: 64})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	const (
		writers = 8
		records = 20
	)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)

	// The writers start together, and the close is released by the first write
	// that lands rather than by the same signal. Released together, the close
	// usually won outright and the test asserted nothing; released after a
	// write has been accepted, it is guaranteed to overlap a writer that is
	// still going.
	var (
		start     = make(chan struct{})
		writing   = make(chan struct{})
		announce  sync.Once
		announced = func() { announce.Do(func() { close(writing) }) }
	)

	for i := range writers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			// The closer is released on every exit path, not only the one that
			// accepts a write. announced is a sync.Once, so in a healthy run
			// this defer does nothing and the close still overlaps a writer
			// that is still going. It matters when no write is ever accepted —
			// a full disk, a revoked mount, an EIO, or a regression returning
			// something other than os.ErrClosed: every writer then takes a
			// t.Errorf branch and returns, wg.Wait proceeds, and without this
			// the closer stays parked on writing forever while the main
			// goroutine blocks on closed. The recorded failure would never be
			// printed; the package would die on the `go test` timeout with a
			// goroutine dump instead, which diagnoses nothing.
			defer announced()

			<-start

			for j := range records {
				line := fmt.Sprintf("writer-%d-record-%d\n", i, j)

				n, err := w.Write([]byte(line))

				switch {
				case err == nil:
					if n != len(line) {
						t.Errorf("accepted write reported %d of %d bytes", n, len(line))

						return
					}

					mu.Lock()
					accepted++
					mu.Unlock()

					announced()
				case errors.Is(err, os.ErrClosed):
					if n != 0 {
						t.Errorf("refused write reported %d bytes written", n)

						return
					}
				default:
					t.Errorf("write racing close: %v", err)

					return
				}
			}
		}()
	}

	closed := make(chan error, 1)

	go func() {
		<-writing

		closed <- w.Close()
	}()

	close(start)
	wg.Wait()

	if err := <-closed; err != nil {
		t.Fatalf("close racing writes: %v", err)
	}

	// Close is still idempotent and still final afterwards.
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if _, err := w.Write([]byte("after\n")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("write after the racing close: %v, want os.ErrClosed", err)
	}

	var total int

	for _, name := range append(archives(t, dir, path), filepath.Base(path)) {
		total += strings.Count(read(t, filepath.Join(dir, name)), "\n")
	}

	mu.Lock()
	defer mu.Unlock()

	if total != accepted {
		t.Fatalf("%d writes were accepted but %d records are on disk", accepted, total)
	}
}

// TestLoggerCloseRacingAStragglingRecordNeverWritesToAClosedHandle is the same
// property at the layer that actually arranges it. recordQueue's worker owns
// the close, the front door calls Logger.Close on every exit path, and a sink
// goroutine that is still finishing has a PostLogger in hand.
//
// The lock order is what is being pinned: safeWriter.mu is held across both the
// forward and the close, and rotatingWriter.mu is only ever taken under it, so
// there is no arrangement in which a record reaches a handle the close has
// already released. A latched os.ErrClosed is exactly what that looks like when
// it goes wrong, and it would otherwise surface to the user as FR-076's one
// warning describing the shutdown rather than a problem.
//
// Post and not PostAsync, and that choice is the test rather than a detail of
// it. PostAsync only enqueues: recordQueue runs a single worker that drains
// q.jobs and calls closeWriter only after that range loop ends, so an
// asynchronous record and the close are strictly ordered on one goroutine and
// cannot overlap by construction. Written that way, this test passes with the
// guard below deleted, which makes it a test of nothing. Post emits on the
// caller's own goroutine, so the forward and the close are two goroutines
// contending for safeWriter.mu, which is the arrangement the guard exists for.
// The production adapter uses PostAsync, but Post is public API and a front
// door that calls it gets no protection from the queue's ordering.
func TestLoggerCloseRacingAStragglingRecordNeverWritesToAClosedHandle(t *testing.T) {
	dir, path := logDir(t)

	logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1})

	const posts = 8

	var wg sync.WaitGroup

	// The writers start together; the close is released by the first record
	// known to have reached the file, not by the same signal. Released
	// together, Logger.Close can win outright — safeWriter.close clears target,
	// so all 160 Posts are then discarded silently, firstErr() is nil because no
	// write ever reached a handle, and the loop over the file below iterates
	// nothing. The test would pass having asserted that a logger which threw
	// every record away did not corrupt anything. That is the same failure
	// TestRotatingWriterCloseRacingAWriteIsWrittenOrRefused documents above, and
	// this is the same fix: a close released only after a record has landed is
	// guaranteed to overlap writers that are still going, and the landed-record
	// assertion after the join turns a vacuous run into a FAIL.
	var (
		start     = make(chan struct{})
		writing   = make(chan struct{})
		announce  sync.Once
		announced = func() { announce.Do(func() { close(writing) }) }
	)

	for i := range posts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Released on every exit path, not only the one that lands a
			// record. If nothing ever lands the closer would otherwise park on
			// writing forever while the main goroutine waits on closed, and the
			// failure this test exists to report would arrive as a package-wide
			// `go test` timeout instead — the hang COR-007 fixed in the sibling.
			defer announced()

			<-start

			landed := false

			for j := range 20 {
				logger.Post(fmt.Sprintf("post-%d-%d", i, j)).
					Info(EventMessageReceived, slog.String("message", strings.Repeat("x", 4096)))

				// Post emits on this goroutine through a bare *os.File with no
				// buffering, so a non-empty log is proof the record reached the
				// handle rather than a discarding writer. Checked until the
				// first one lands, then never again: this is inside the hot
				// loop the close has to overlap.
				if !landed && sizeOf(path) > 0 {
					landed = true

					announced()
				}
			}
		}()
	}

	closed := make(chan error, 1)

	go func() {
		<-writing

		closed <- logger.Close()
	}()

	close(start)
	wg.Wait()

	// A flush timeout is this logger's documented behaviour when the close has
	// to wait for a destination the writers are hammering, and it says nothing
	// about the race; any other error does. errRecordQueueFull is not tolerated,
	// because nothing is admitted to the queue on this path — seeing it would
	// mean the test had stopped exercising the synchronous emission it is for.
	if err := <-closed; err != nil && !errors.Is(err, errRecordFlushTimeout) {
		t.Fatalf("Logger.Close racing emission: %v", err)
	}

	if err := logger.writer.firstErr(); errors.Is(err, os.ErrClosed) {
		t.Fatalf("a straggling record was written to a closed handle: %v", err)
	}

	// And whatever did land is still one whole JSON object per line: a record
	// interleaved with the close would be a truncated one.
	landed := 0

	for _, name := range append(archives(t, dir, path), filepath.Base(path)) {
		for _, line := range strings.Split(strings.TrimSuffix(read(t, filepath.Join(dir, name)), "\n"), "\n") {
			if line == "" {
				continue
			}

			landed++

			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("line in %s is not a JSON object: %v", name, err)
			}
		}
	}

	// The two assertions above are both satisfied by a log nothing was ever
	// written to, so this one has to be positive. The close cannot begin until
	// a record has landed, so an empty log means either that the release order
	// broke or that emission stopped reaching the handle at all — in both cases
	// everything above asserted nothing and the run must be red, not green.
	if landed == 0 {
		t.Fatal("no record reached the log: the assertions above passed vacuously")
	}
}

// TestRotatedFilesKeepTheLogPermissions pins that an archive is no more
// readable than the active log, which carries the user's own message bodies.
func TestRotatedFilesKeepTheLogPermissions(t *testing.T) {
	dir, path := logDir(t)

	w, err := openRotating(path, rotationOptions{maxSize: 1})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")
	write(t, w, "b")

	for _, name := range append(archives(t, dir, path), filepath.Base(path)) {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}

		if got := info.Mode().Perm(); got != logFilePerm {
			t.Fatalf("%s is mode %v, want %v", name, got, logFilePerm)
		}
	}
}

// TestRotateThresholdConversion pins the unit conversion and, more importantly,
// the clamp. config.Validate bounds both keys from below only, so a value near
// the integer limit reaches these functions, and an unclamped multiplication
// wraps negative — which does not disable rotation, it makes every write rotate.
//
// Every expectation here is a literal. A table written in bytesPerMiB or
// dayDuration restates the implementation and passes whatever those constants
// say; written out, one MiB is 1048576 bytes and one day is 24 hours or the
// conversion is wrong.
//
// The largest inputs the clamp distinguishes need an int wider than 32 bits to
// write down, so the size clamp's own boundary lives in
// rotate_size_clamp_64bit_test.go and the rest of this file stays compilable
// everywhere internal/logging builds.
func TestRotateThresholdConversion(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		cases := map[int]int64{
			0:  0,
			-1: 0,
			1:  1048576,
			10: 10485760,
		}

		for mib, want := range cases {
			if got := rotateSizeBytes(mib); got != want {
				t.Errorf("rotateSizeBytes(%d) = %d, want %d", mib, got, want)
			}
		}
	})

	t.Run("age", func(t *testing.T) {
		cases := map[int]time.Duration{
			0:  0,
			-1: 0,
			1:  24 * time.Hour,
			7:  168 * time.Hour,
			// The clamp's boundary. maxRotateAfterDays fits an int on every
			// platform this builds on — it is about a hundred thousand days —
			// so unlike the size clamp it needs no separate file.
			int(maxRotateAfterDays):     time.Duration(maxRotateAfterDays) * dayDuration,
			int(maxRotateAfterDays) + 1: time.Duration(math.MaxInt64),
		}

		for days, want := range cases {
			if got := rotateAge(days); got != want {
				t.Errorf("rotateAge(%d) = %v, want %v", days, got, want)
			}
		}
	})

	t.Run("no setting an int can hold wraps negative", func(t *testing.T) {
		// The wrap is the reason the clamp exists, and a negative threshold is
		// not an inert one: dueForRotation compares >=, so it rotates on every
		// write. math.MaxInt rather than math.MaxInt64 so this runs as written
		// on a 32-bit int too, where it is still the largest reachable setting.
		for _, mib := range []int{1, 1 << 20, math.MaxInt / 2, math.MaxInt - 1, math.MaxInt} {
			if got := rotateSizeBytes(mib); got < 0 {
				t.Errorf("rotateSizeBytes(%d) wrapped to %d, which rotates on every write", mib, got)
			}
		}

		for _, days := range []int{1, 365, math.MaxInt / 2, math.MaxInt - 1, math.MaxInt} {
			if got := rotateAge(days); got < 0 {
				t.Errorf("rotateAge(%d) wrapped to %v, which rotates on every write", days, got)
			}
		}
	})

	t.Run("a wrapping setting does not rotate", func(t *testing.T) {
		dir, path := logDir(t)

		w, err := openRotating(path, rotationOptions{
			maxSize: rotateSizeBytes(math.MaxInt),
			maxAge:  rotateAge(math.MaxInt),
		})
		if err != nil {
			t.Fatalf("open the rotating writer: %v", err)
		}

		t.Cleanup(func() { _ = w.Close() })

		for range 10 {
			write(t, w, "record\n")
		}

		if got := archives(t, dir, path); len(got) != 0 {
			t.Fatalf("an absurd threshold rotated anyway: %v", got)
		}
	})
}

// TestCreationTimeFallsBackWithoutPanicking pins the guarded type assertion.
// FileInfo.Sys holds whatever the system supplies, and a panic on the
// diagnostics path is the one thing FR-076 cannot tolerate.
func TestCreationTimeFallsBackWithoutPanicking(t *testing.T) {
	modified := time.Date(2026, 3, 4, 5, 6, 7, 0, time.Local)

	if got := creationTime(stubInfo{modTime: modified}); !got.Equal(modified) {
		t.Fatalf("creationTime with no Stat_t = %s, want the modification time %s", got, modified)
	}
}

// stubInfo is a FileInfo whose Sys carries nothing the platform recognises.
type stubInfo struct {
	modTime time.Time
	sys     any
}

func (s stubInfo) Name() string       { return "app.jsonl" }
func (s stubInfo) Size() int64        { return 0 }
func (s stubInfo) Mode() fs.FileMode  { return logFilePerm }
func (s stubInfo) ModTime() time.Time { return s.modTime }
func (s stubInfo) IsDir() bool        { return false }
func (s stubInfo) Sys() any           { return s.sys }

// TestOpenRotatesARealLogFile is the end-to-end half of T065: a Logger built
// the way a front door builds one, writing real records to a real file, until
// the configured threshold is crossed. Everything above tests the writer's
// decisions; this tests that they are wired to anything at all.
func TestOpenRotatesARealLogFile(t *testing.T) {
	dir, path := logDir(t)

	logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1})

	// Large records so one megabyte is a few hundred of them rather than tens
	// of thousands.
	body := strings.Repeat("x", 4096)

	const records = 300

	for i := range records {
		logger.Post(fmt.Sprintf("post-%d", i)).Info(EventMessageReceived, slog.String("message", body))
	}

	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("rotation reported itself as a degradation: %s", degraded.Warning())
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("close the logger: %v", err)
	}

	rotated := archives(t, dir, path)
	if len(rotated) == 0 {
		t.Fatal("no archive: 1.2 MiB of records were written with a 1 MiB threshold")
	}

	for _, name := range rotated {
		if !regexp.MustCompile(`^app\.jsonl\.[0-9]{14}(-[0-9]+)?$`).MatchString(name) {
			t.Fatalf("archive %q does not carry FR-073's suffix", name)
		}
	}

	// Not one record is lost across the rotation, and every line is still a
	// self-contained JSON object (FR-064).
	var seen int

	for _, name := range append(rotated, filepath.Base(path)) {
		for _, line := range strings.Split(strings.TrimSuffix(read(t, filepath.Join(dir, name)), "\n"), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("line in %s is not a JSON object: %v", name, err)
			}

			if record[keyMessageID] == nil {
				t.Fatalf("line in %s lost its correlation identifier", name)
			}

			seen++
		}
	}

	if seen != records {
		t.Fatalf("recovered %d records across the log and its archives, want %d", seen, records)
	}
}

// TestOpenWithASuppliedWriterDoesNotRotate pins that the Writer seam opts out
// of rotation entirely: a supplied destination owns its own lifecycle, and this
// package must not create files next to a path it was told not to open.
func TestOpenWithASuppliedWriterDoesNotRotate(t *testing.T) {
	dir, path := logDir(t)

	var sink strings.Builder

	logger := Open(Options{Path: path, Source: SourceCLI, Writer: &sink, RotateSizeMiB: 1, RotateAfterDays: 1})

	for i := range 50 {
		logger.Post(fmt.Sprintf("post-%d", i)).Info(EventMessageReceived, slog.String("message", strings.Repeat("x", 4096)))
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("close the logger: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the log directory: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("a supplied writer touched the filesystem: %v", entries)
	}

	if sink.Len() == 0 {
		t.Fatalf("the supplied writer received nothing")
	}
}

// TestRotationFailureBecomesTheOneWarning is FR-076 through the whole stack: a
// rotation that cannot proceed must not stop the records, must not panic, and
// must reach the front door as the single warning naming the log path.
func TestRotationFailureBecomesTheOneWarning(t *testing.T) {
	dir, path := logDir(t)

	// Already past the 1 MiB threshold, so the very first record rotates.
	if err := os.WriteFile(path, make([]byte, 2<<20), logFilePerm); err != nil {
		t.Fatalf("seed an oversized log: %v", err)
	}

	logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1})

	t.Cleanup(func() { _ = logger.Close() })

	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("degraded before anything failed: %s", degraded.Warning())
	}

	// A read-only directory is the simplest real cause: no name can be claimed.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("make the directory read-only: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	logger.Post("post-1").Info(EventMessageReceived)

	degraded := logger.Degraded()
	if degraded == nil {
		t.Fatal("a failed rotation raised no warning")
	}

	if degraded.Path != path {
		t.Fatalf("warning names %q, want the log path %q", degraded.Path, path)
	}

	if !strings.Contains(degraded.Warning(), "claim a rotated log name") {
		t.Fatalf("warning = %q, want it to say what failed", degraded.Warning())
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore the directory: %v", err)
	}

	// And the record still reached disk: a rotation that could not happen is a
	// log that is too big, not a log that drops records.
	if !strings.Contains(read(t, path), `"event":"message_received"`) {
		t.Fatal("the record was dropped when rotation failed")
	}
}

// TestOpenRotatesAnAgedLogFile is the age half of the end-to-end test. It
// cannot use the fake clock — Open owns the writer's construction — so the log
// is backdated instead: setting the modification time earlier than the creation
// time carries the creation time back with it on darwin, and every other
// platform reads the modification time as the creation time anyway.
//
// Without it, an Open that dropped the age threshold on the floor would be
// caught only by the front door's own wiring test in internal/app, which is a
// long way from the package that owns the rule.
func TestOpenRotatesAnAgedLogFile(t *testing.T) {
	dir, path := logDir(t)

	if err := os.WriteFile(path, []byte("from a month ago\n"), logFilePerm); err != nil {
		t.Fatalf("seed the log: %v", err)
	}

	old := time.Now().Add(-30 * dayDuration)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("backdate the log: %v", err)
	}

	// A size threshold far out of reach, so only age can explain a rotation.
	logger := Open(Options{Path: path, Source: SourceCLI, RotateSizeMiB: 1024, RotateAfterDays: 7})

	logger.Post("post-1").Info(EventMessageReceived)

	if degraded := logger.Degraded(); degraded != nil {
		t.Fatalf("degraded: %s", degraded.Warning())
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("close the logger: %v", err)
	}

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one: a 30-day-old log with a 7-day threshold", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "from a month ago\n" {
		t.Fatalf("archive = %q, want the old log preserved intact", got)
	}

	if !strings.Contains(read(t, path), `"event":"message_received"`) {
		t.Fatalf("the new active log did not receive the record")
	}
}
