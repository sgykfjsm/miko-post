package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sync"
	"time"
)

// rotationSuffixLayout is FR-073's suffix, in local time: app.jsonl becomes
// app.jsonl.20260827114203.
//
// Local and not UTC, deliberately and because the requirement says so. These
// files are read by the one person running the tool, usually to find "the log
// from around when that post failed", and a suffix in a zone that is not theirs
// turns a directory listing into an arithmetic problem.
const rotationSuffixLayout = "20060102150405"

// bytesPerMiB converts the configured size threshold, which is in MiB because
// that is the unit contracts/config-schema.md gives the user.
const bytesPerMiB = 1 << 20

// dayDuration is the unit of rotate_after_days.
//
// A fixed 24 hours rather than a calendar day. The threshold is an age, not a
// date, so a DST transition must not make "7 days" mean 7 days minus an hour;
// time.Duration cannot represent a calendar day anyway.
const dayDuration = 24 * time.Hour

// Largest thresholds that survive conversion into the units this file works in.
//
// This is not hypothetical tidiness. config.Validate bounds both keys from
// below only — rotate_size_mib and rotate_after_days must be greater than zero
// and nothing caps them — so a value near the integer limit reaches here, and
// the multiplication below would wrap it negative. A negative threshold is not
// an inert one: dueForRotation compares size >= threshold, so every write would
// rotate, producing a directory of one-record files rather than a log. Clamping
// makes an absurd setting mean "effectively never", which is the direction that
// loses nothing.
//
// Typed int64 rather than untyped: maxRotateSizeMiB exceeds a 32-bit int, and
// an untyped constant compared against an int would not compile there.
const (
	maxRotateSizeMiB   int64 = math.MaxInt64 / bytesPerMiB
	maxRotateAfterDays int64 = math.MaxInt64 / int64(dayDuration)
)

// maxCollisionAttempts bounds the -1, -2, … search for a free rotated name.
//
// Bounded because this runs inside a log write, and FR-076's first duty is that
// diagnostics never block a post: a directory that somehow held every candidate
// name would otherwise spin here forever. A thousand rotations inside one clock
// second is already far past anything this tool can produce, so the bound is
// only ever reached by a pathological directory, and reaching it degrades
// rather than overwriting (FR-074).
const maxCollisionAttempts = 1000

// rotationOptions are the knobs openRotating takes.
//
// A struct rather than five positional parameters because now and stat are test
// seams that production never sets, and because maxSize and maxAge are two
// numbers of different units that would be easy to transpose at a call site.
type rotationOptions struct {
	// maxSize is the size threshold in bytes; zero or less disables the size
	// trigger.
	maxSize int64

	// maxAge is the age threshold; zero or less disables the age trigger.
	maxAge time.Duration

	// now defaults to time.Now.
	now func() time.Time

	// stat reads the freshly opened active file's size and creation time, and
	// defaults to (*os.File).Stat.
	stat func(*os.File) (os.FileInfo, error)
}

// rotatingWriter is the active log file plus the bookkeeping FR-072 needs:
// the size so far and the creation time captured when the file was opened.
//
// It owns the file's whole lifecycle — it decides before every write whether to
// rename and reopen — which is why it is a writer rather than something layered
// over a handle held elsewhere. Open hands it to safeWriter as both the target
// and the owned closer.
//
// Nothing here ever deletes, truncates, expires or compresses a rotated file
// (FR-074, constitution principle VI). The only os.Remove in this file removes
// a zero-byte placeholder this writer created moments earlier and failed to
// rename onto; see rotate.
type rotatingWriter struct {
	path    string
	maxSize int64
	maxAge  time.Duration
	now     func() time.Time
	stat    func(*os.File) (os.FileInfo, error)

	// mu guards everything below, and covers a whole Write — rotation and the
	// write it precedes are one decision and must not interleave with another.
	//
	// safeWriter already serialises writes arriving through the Logger, so in
	// production this is never contended. It is held anyway because Close can
	// arrive from a different goroutine than a straggling write, and because a
	// type that is only safe when its caller happens to be is a trap for the
	// next caller.
	mu        sync.Mutex
	file      *os.File
	size      int64
	createdAt time.Time
	closed    bool
}

// openRotating opens the active log and captures the bookkeeping rotation
// needs. An error means no log file; the caller degrades (FR-075, FR-076).
func openRotating(path string, opts rotationOptions) (*rotatingWriter, error) {
	w := &rotatingWriter{
		path:    path,
		maxSize: opts.maxSize,
		maxAge:  opts.maxAge,
		now:     opts.now,
		stat:    opts.stat,
	}

	if w.now == nil {
		w.now = time.Now
	}

	if w.stat == nil {
		w.stat = (*os.File).Stat
	}

	if err := w.open(); err != nil {
		return nil, err
	}

	return w, nil
}

// open attaches a fresh active file and resets the bookkeeping to describe it.
//
// openLogFile and not os.OpenFile: the non-regular-file refusal and the
// O_APPEND that keeps earlier runs intact are obligations of every handle on
// this path, not just the first one, and a rotation that reopened the path
// directly would drop both the moment the log was replaced mid-run by a FIFO or
// a directory. Re-creating the log directory on every rotation is the same
// bargain and costs one syscall a few times a day.
func (w *rotatingWriter) open() error {
	file, err := openLogFile(w.path)
	if err != nil {
		return err
	}

	info, err := w.stat(file)
	if err != nil {
		// A handle with no size and no creation time cannot honour either of
		// FR-072's conditions, so it is refused rather than used: writing
		// through it would look healthy while the log grew without bound. The
		// caller turns this into the one warning FR-076 allows, and the post
		// runs either way.
		//
		// The file is closed rather than leaked; its content is untouched.
		_ = file.Close()

		return fmt.Errorf("measure the log file: %w", err)
	}

	w.file = file
	w.size = info.Size()
	w.createdAt = creationTime(info)

	// An empty file is dated from the clock the age comparison uses, not from
	// the one the filesystem answers with.
	//
	// dueForRotation subtracts two independent time sources, and nothing keeps
	// them together: a log directory on a network mount whose server clock runs
	// days behind, or a darwin volume reporting a non-zero but nonsensical
	// Birthtimespec, makes a file created a microsecond ago already older than
	// the threshold. Without this, a rotation does not clear its own trigger —
	// the replacement is born expired, so the next write rotates it too, and
	// every single record becomes its own file behind a zero-byte archive. That
	// is unbounded, and it is the shape FR-074 and constitution principle VI
	// keep the log out of.
	//
	// It costs nothing because the file is empty: there are no records whose
	// age this could misreport, and rotating an empty log would archive
	// nothing. A file that already holds records keeps the filesystem's answer,
	// which is what lets a genuinely old log rotate on the first write after a
	// restart (FR-072, R-006, A-011).
	if w.size == 0 {
		w.createdAt = w.now()
	}

	return nil
}

// dueForRotation reports whether either of FR-072's two conditions holds.
//
// Both are computed before the || rather than short-circuited. The requirement
// is that both are evaluated before each write, the cost is one clock reading,
// and writing it this way means the age condition cannot be silently dropped by
// a later edit that reorders the two.
//
// A threshold of zero or less disables its condition instead of matching
// everything. config.Validate rejects both keys at zero, so the only way to
// arrive here with one is a caller that did not set it — the tests, and any
// front door with a wiring bug — and for those "do not rotate" is the answer
// that loses nothing, where "rotate on every write" would shred the log.
//
// The age condition subtracts one clock from another, and the two are only
// comparable because open says so: it dates an empty file from this same now(),
// so a file this writer just created cannot already be expired however
// implausible a creation time the filesystem reports. See open.
func (w *rotatingWriter) dueForRotation() bool {
	oversize := w.maxSize > 0 && w.size >= w.maxSize
	expired := w.maxAge > 0 && w.now().Sub(w.createdAt) >= w.maxAge

	return oversize || expired
}

// Write evaluates both rotation conditions, rotates if either holds, and then
// writes (FR-072).
//
// A rotation failure is reported but does not suppress the write: the record
// still lands in whatever active file exists, and the error travels up to
// safeWriter, which latches it as the reason for FR-076's single warning. That
// ordering is the whole design — a log that could not be rotated is a log that
// is too big, not a log that has to start dropping records.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}

	var rotateErr error

	if w.file != nil && w.dueForRotation() {
		rotateErr = w.rotate()
	}

	if w.file == nil {
		// A previous rotation archived the active file and could not open its
		// replacement. Retrying here rather than giving up for the life of the
		// process is what makes that recoverable: writes are a handful per post,
		// and nothing else would ever reopen the log.
		if err := w.open(); err != nil {
			return 0, errors.Join(rotateErr, err)
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)

	return n, errors.Join(rotateErr, err)
}

// rotate archives the active file under its timestamped name and opens a fresh
// one (FR-073, FR-074).
//
// Every failure path here leaves the log readable and leaves every existing
// rotated file untouched. Nothing is written with O_TRUNC and nothing that
// holds records is removed.
func (w *rotatingWriter) rotate() error {
	target, err := w.claimName()
	if err != nil {
		// Nothing has moved. The active file is still open and still the active
		// file, so the write this precedes lands in it and the log simply grows
		// past its threshold until the cause clears. That is the lossless
		// direction to fail in.
		return err
	}

	if err := os.Rename(w.path, target); err != nil {
		// Same: the active file is untouched. The claim is a zero-byte
		// placeholder claimName created moments ago with O_EXCL, so removing it
		// deletes no rotated log (FR-074) and no file this process did not
		// create. Leaving it would burn one timestamp name per failed write and
		// litter the directory with empty files that look like archives.
		_ = os.Remove(target)

		return fmt.Errorf("rotate the log to %s: %w", target, err)
	}

	// The rename does not disturb the open handle — it still refers to the
	// archived file — so every record written before this point is safe on disk
	// under the new name before anything else happens.
	old := w.file
	w.file = nil

	closeErr := old.Close()

	if err := w.open(); err != nil {
		// The records are archived and intact; what is missing is somewhere to
		// put the next ones. file stays nil so the next Write retries the open,
		// and this error becomes FR-076's one warning in the meantime.
		return errors.Join(fmt.Errorf("open a new active log after rotating to %s: %w", target, err), closeErr)
	}

	return closeErr
}

// claimName reserves the name the active file will be renamed to, appending
// -1, -2, … until one is free (FR-073, R-006, A-009).
//
// The name is claimed by creating it with O_CREATE|O_EXCL rather than by
// asking whether it exists and then renaming onto it. Those are not equivalent:
// the second is a check-then-act on a path any other process — a second mp, a
// backup tool — can fill in between, and losing that race silently overwrites a
// rotated log, which is exactly the outcome FR-074 and constitution principle
// VI forbid. O_EXCL makes the claim and the check the same syscall, so two
// racing rotations get two different names.
//
// The placeholder is then renamed over by the caller, which is a replacement of
// this call's own empty file and not of anyone's data.
func (w *rotatingWriter) claimName() (string, error) {
	base := w.path + "." + w.now().Format(rotationSuffixLayout)

	for attempt := 0; attempt <= maxCollisionAttempts; attempt++ {
		name := base

		if attempt > 0 {
			name = fmt.Sprintf("%s-%d", base, attempt)
		}

		file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, logFilePerm)

		switch {
		case err == nil:
			// The claim is made by the create, not by the handle: this file is
			// empty and the caller is about to rename the active log onto it,
			// which replaces the inode whatever close reports. So the result is
			// deliberately discarded rather than turned into a rotation
			// failure — close releases the descriptor either way, and degrading
			// diagnostics over it would be a warning about nothing.
			_ = file.Close()

			return name, nil
		case errors.Is(err, fs.ErrExist):
			// Taken by a rotated log or by a racing rotation. Never overwritten
			// (FR-074): try the next number.
			continue
		default:
			return "", fmt.Errorf("claim a rotated log name: %w", err)
		}
	}

	return "", fmt.Errorf("claim a rotated log name: %s and %d numbered variants are all taken",
		base, maxCollisionAttempts)
}

// Close releases the active file once. Later calls do nothing, and a write
// after it is refused rather than silently reopening the log.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true

	if w.file == nil {
		return nil
	}

	file := w.file
	w.file = nil

	return file.Close()
}

// rotateSizeBytes converts rotate_size_mib into the bytes dueForRotation
// compares against, clamping a value that would wrap. See maxRotateSizeMiB.
func rotateSizeBytes(mib int) int64 {
	switch {
	case mib <= 0:
		return 0
	case int64(mib) > maxRotateSizeMiB:
		return math.MaxInt64
	default:
		return int64(mib) * bytesPerMiB
	}
}

// rotateAge converts rotate_after_days into a duration, clamping a value that
// would wrap. See maxRotateAfterDays.
func rotateAge(days int) time.Duration {
	switch {
	case days <= 0:
		return 0
	case int64(days) > maxRotateAfterDays:
		return time.Duration(math.MaxInt64)
	default:
		return time.Duration(days) * dayDuration
	}
}
