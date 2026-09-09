package obsidian

import (
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// NewWithClock builds a Sink reading the clock through clock, for the external
// test package and only for it.
//
// The clock has to be substitutable for two properties that cannot be asserted
// any other way. FR-051 puts the note name and the entry timestamp in local
// time, and issue #98 requires both to come from *one* reading — a claim about
// what happens when a post spans local midnight, which a test cannot make by
// waiting. And the entry line contains a rendered time, so an assertion on the
// exact bytes written needs to know what that time is.
//
// Following internal/config/export_test.go and internal/logging/export_test.go:
// this file is compiled into the test binary alone, so the shipped API does not
// grow a clock parameter to make the clock testable.
func NewWithClock(settings config.ObsidianSettings, clock func() time.Time) *Sink {
	sink := New(settings)
	sink.now = clock

	return sink
}

// AppendEntry exposes the write-and-close step so its I/O failure paths are
// reachable.
//
// Those paths — a write that fails after a successful open, a short write, a
// close that fails — cannot be provoked portably through the filesystem: they
// need a full disk, a revoked mount, or a filesystem that defers its flush. They
// are also the paths that decide whether a partially written line is reported to
// the user as a success, which in the component whose whole job is not losing
// their writing is not something to leave to inspection.
var AppendEntry = appendEntry

// NoteHandle is the interface AppendEntry writes through, so a test can supply
// a failing one.
type NoteHandle = noteHandle

// RefuseUnlessRegular exposes the post-open check on the note's type.
//
// Same reason as AppendEntry: its fstat-failure arm needs a descriptor that
// os.OpenFile returned and that then cannot answer fstat — a revoked mount or
// failing media — which no test can arrange portably. That arm decides both the
// error the user reads and whether the descriptor is released, and the FIFO
// case in sink_unix_test.go can only reach the other one.
var RefuseUnlessRegular = refuseUnlessRegular
