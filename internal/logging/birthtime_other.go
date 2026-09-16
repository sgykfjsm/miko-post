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
// It is the conservative substitute rather than an equivalent. ModTime is never
// earlier than the creation time, so the age this yields is never larger than
// the true age: the active log rotates no sooner than it should, and for the
// append-only file this package owns the two values coincide until the first
// write after the process starts. The failure mode is a log that rotates later
// than a reader expects, never one that rotates early or loses anything.
//
// See birthtime_darwin.go for the platform this tool actually targets, where
// the real birth time is available.
func creationTime(info os.FileInfo) time.Time {
	return info.ModTime()
}
