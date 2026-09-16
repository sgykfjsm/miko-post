//go:build darwin

package logging

import (
	"os"
	"syscall"
	"time"
)

// creationTime reports when the file was created, which is what FR-072
// measures the active log's age from (R-006, A-011).
//
// darwin records a real birth time, so on the platform this tool targets the
// age trigger measures what the requirement says it measures rather than an
// approximation of it. Everywhere else the portable file answers with ModTime;
// isolating the one syscall here is what keeps the rest of the package
// buildable and testable on any platform.
//
// The type assertion is guarded rather than taken. FileInfo.Sys returns any,
// and its content is documented as whatever the underlying system supplies —
// so a filesystem or a future os change that puts something else there would
// panic on the diagnostics path, which is precisely what T023 and FR-076
// forbid: a logging detail must never take down a post.
//
// A zero Birthtimespec is treated as "not recorded" and falls back the same
// way. Both fallbacks err safely in the direction that matters, but only
// conditionally: ModTime is not earlier than the creation time unless something
// set it, and a preserved-timestamp copy (cp -p, rsync -a, tar -x) or a restore
// from backup does exactly that. Absent that, the measured age is no larger
// than the true age and the file rotates no sooner than it should. Rotating
// early is the dangerous direction — it would split a log for no reason, and on
// a file rewritten by something else could do it on every write. Two things
// bound it here: APFS clamps a file's birth time down to a backdated mtime
// rather than letting mtime fall behind it, so the volumes that populate
// Birthtimespec cannot reach the case at all (birthtime_darwin_test.go relies
// on that clamp), and open()'s w.size == 0 branch dates an empty file from the
// process clock, so even a backdated log rotates once and its replacement is
// born un-expired instead of rotating on every write.
//
// "No sooner than it should" understates what the fallback costs, though. A
// volume that records no birth time — SMB, NFS, exFAT — puts this build in
// exactly the position birthtime_other.go describes: the handle is O_APPEND and
// createdAt is captured once per open, not once per process, so mp's CLI front
// door — which opens the log, posts and exits — measures the log's age from the
// previous run's last post, and a user posting more often than
// rotate_after_days never sees the age condition fire from the CLI at all.
//
// The GUI is the exception, which is why this says "per open" and not "per
// run": internal/gui/run.go opens one Logger for the whole session, so a window
// left open longer than rotate_after_days does fire the age condition, measured
// from session start. Nothing is lost either way and rotate_size_mib still
// bounds the file; what is degraded is the age trigger, which on such a volume
// measures the age of the open handle rather than of the log. See
// birthtime_other.go and the open question DEC-ADV-006, recorded in
// .agents/state.yaml under batch 10a.
func creationTime(info os.FileInfo) time.Time {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}

	sec, nsec := stat.Birthtimespec.Unix()
	if sec == 0 && nsec == 0 {
		return info.ModTime()
	}

	return time.Unix(sec, nsec)
}
