//go:build !darwin

package logging

import (
	"os"
	"time"
)

// creationTime reports the best available answer to "when did this file begin"
// on platforms with no portable birth time (FR-072, R-006, A-011).
//
// Go exposes no creation time in os.FileInfo, and the platforms that record one
// disagree on how to reach it — Linux needs statx and a recent enough kernel
// and filesystem, Windows has it in a different Sys() type. ModTime is the one
// answer every platform gives.
//
// It is a substitute rather than an equivalent, and the direction it errs in is
// conditional, not guaranteed. ModTime is not earlier than the creation time
// unless something set it — and on exactly the platforms this file serves,
// something can: utimensat writes an arbitrary modification time, so a
// preserved-timestamp copy (cp -p, rsync -a, tar -x) or a log restored from a
// backup leaves an mtime behind the inode's real creation, and the first write
// after the restore rotates immediately. Absent that, the age this yields is no
// larger than the true age and the log rotates no sooner than it should.
//
// Neither case loses anything: this fallback never deletes, truncates or
// overwrites (FR-074), and open()'s w.size == 0 branch bounds the backdated
// case to a single rotation rather than a per-write cascade, because the
// replacement log is empty and is therefore dated from the process clock and
// born un-expired. darwin cannot reach the backdated case at all — APFS clamps
// a file's birth time down to a backdated mtime instead of letting mtime fall
// behind it, which is the property birthtime_darwin_test.go relies on.
//
// What it costs in the ordinary case is the age trigger itself, not a few hours
// of it. Within one process the two values coincide until the first write,
// which is why nothing in a single run looks wrong — but the handle is O_APPEND
// so every post moves the modification time to now, and createdAt is captured
// once per open, not once per process. mp's CLI front door opens the log, posts
// and exits, so the next run measures the log's age from the previous run's
// last post: a user who posts more often than rotate_after_days never sees
// FR-072's age condition fire from the CLI, however old the log really is.
//
// The GUI is the exception, and it is why this says "per open" rather than "per
// run". internal/gui/run.go opens one Logger for the whole session and closes
// it when the window does, so createdAt is captured at session start and now()
// advances for the life of the window: a session left open longer than
// rotate_after_days does fire the age condition, measured from session start
// rather than from the log's real age. The trigger therefore works more often
// than "never" — what it measures on this build is the age of the open handle.
//
// rotate_size_mib still bounds the file, so the consequence is a log that grows
// to its size threshold instead of rotating by age — not one that loses
// records. Whether that is acceptable, or whether this needs statx, a Windows
// path, or a recorded creation time beside the log, is a product decision and
// not this file's to take. It is filed, unanswered by design, as the open
// question DEC-ADV-006 in .agents/state.yaml under batch 10a.
//
// See birthtime_darwin.go for the platform this tool actually targets, where
// the real birth time is available.
func creationTime(info os.FileInfo) time.Time {
	return info.ModTime()
}
