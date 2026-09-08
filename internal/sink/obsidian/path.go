package obsidian

import (
	"path/filepath"
	"time"
)

// DailyNotePath resolves the note a message posted at `at` belongs in
// (FR-044, FR-051).
//
//	<dir>/<at.Format(filenameFormat)>
//
// The instant is a parameter rather than being read here, and that is the whole
// point of this function's shape. FR-051 puts every date and time rendering for
// this sink in local time, and two things then need to agree: the note this
// message is written to, and the note the diagnostic log says it was written to
// (issue #98). A second time.Now() anywhere in that chain makes them disagree
// across local midnight — the post lands in yesterday's note and the log names
// today's, which is exactly the reconstruction trail SC-008 exists to
// guarantee. One instant, resolved once, passed everywhere.
//
// filepath.Join cleans the result, so a directory with a trailing separator or
// an interior "." behaves the same as one without. It does not make a relative
// directory absolute: config requires daily_note_dir to be absolute when the
// sink is enabled, because the working directory belongs to whichever front
// door happened to launch.
//
// A filenameFormat containing a path separator would place the note in a
// subdirectory, which Join treats as ordinary path syntax rather than an error.
// That is left to the settings layer to reject or permit; this function
// resolves what it is given.
func DailyNotePath(dir, filenameFormat string, at time.Time) string {
	return filepath.Join(dir, at.Format(filenameFormat))
}
