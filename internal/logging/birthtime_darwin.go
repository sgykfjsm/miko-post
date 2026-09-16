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
// way. Both fallbacks are safe in one direction and that is the direction they
// take: ModTime is never earlier than the creation time, so the measured age is
// never larger than the true age and the file rotates no sooner than it should.
// Rotating late costs a larger log; rotating early would split a log for no
// reason, and on a file rewritten by something else could do it on every write.
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
