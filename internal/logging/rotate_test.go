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
type flakyStat struct {
	mu    sync.Mutex
	calls int
	fail  map[int]error
}

func (f *flakyStat) stat(file *os.File) (os.FileInfo, error) {
	f.mu.Lock()
	f.calls++
	err := f.fail[f.calls]
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

// TestRotateAgeTriggerFiresIndependentlyOfSize pins FR-072's second condition.
// The file is one byte long, so only age can explain the rotation.
func TestRotateAgeTriggerFiresIndependentlyOfSize(t *testing.T) {
	dir, path := logDir(t)

	clock := &fixedClock{now: time.Now()}

	w, err := openRotating(path, rotationOptions{maxAge: 7 * dayDuration, now: clock.Now})
	if err != nil {
		t.Fatalf("open the rotating writer: %v", err)
	}

	t.Cleanup(func() { _ = w.Close() })

	write(t, w, "a")

	if got := archives(t, dir, path); len(got) != 0 {
		t.Fatalf("rotated before the age threshold: %v", got)
	}

	// Exactly at the threshold: the comparison is >=, so this rotates.
	clock.set(w.createdAt.Add(7 * dayDuration))

	write(t, w, "b")

	rotated := archives(t, dir, path)
	if len(rotated) != 1 {
		t.Fatalf("archives = %v, want exactly one", rotated)
	}

	if got := read(t, filepath.Join(dir, rotated[0])); got != "a" {
		t.Fatalf("archive = %q, want %q", got, "a")
	}

	if got := read(t, path); got != "b" {
		t.Fatalf("new active log = %q, want %q", got, "b")
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
// package's refusal rather than reimplementing the open: a FIFO at the log path
// blocks inside open(2) and would stop the post dead.
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
func TestRotateThresholdConversion(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		cases := map[int]int64{
			0:                         0,
			-1:                        0,
			1:                         1 << 20,
			10:                        10 << 20,
			int(maxRotateSizeMiB):     maxRotateSizeMiB * bytesPerMiB,
			int(maxRotateSizeMiB) + 1: math.MaxInt64,
			math.MaxInt64:             math.MaxInt64,
		}

		for mib, want := range cases {
			if got := rotateSizeBytes(mib); got != want {
				t.Errorf("rotateSizeBytes(%d) = %d, want %d", mib, got, want)
			}

			if got := rotateSizeBytes(mib); got < 0 {
				t.Errorf("rotateSizeBytes(%d) wrapped to %d, which rotates on every write", mib, got)
			}
		}
	})

	t.Run("age", func(t *testing.T) {
		cases := map[int]time.Duration{
			0:                           0,
			-1:                          0,
			1:                           dayDuration,
			7:                           7 * dayDuration,
			int(maxRotateAfterDays):     time.Duration(maxRotateAfterDays) * dayDuration,
			int(maxRotateAfterDays) + 1: time.Duration(math.MaxInt64),
			math.MaxInt64:               time.Duration(math.MaxInt64),
		}

		for days, want := range cases {
			if got := rotateAge(days); got != want {
				t.Errorf("rotateAge(%d) = %v, want %v", days, got, want)
			}

			if got := rotateAge(days); got < 0 {
				t.Errorf("rotateAge(%d) wrapped to %v, which rotates on every write", days, got)
			}
		}
	})

	t.Run("a wrapping setting does not rotate", func(t *testing.T) {
		dir, path := logDir(t)

		w, err := openRotating(path, rotationOptions{
			maxSize: rotateSizeBytes(math.MaxInt64),
			maxAge:  rotateAge(math.MaxInt64),
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
