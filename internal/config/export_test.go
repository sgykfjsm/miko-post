package config

import "time"

// ValidateAt exposes Validate's wall-clock seam to the package's external test
// package, and only to it — this file is compiled into the test binary alone,
// so nothing in the shipped API grows in order to make the instant testable.
//
// What it is for: asserting that layout detection ignores the instant it is
// handed, and that the accepted set does not move across a real zone
// transition. Both are claims about an instant the caller has to be able to
// state, and neither is a claim the shipped API should let a caller make.
//
// The alternative spellings are both unsound. $TZ is read once, when Go first
// resolves time.Local, which has usually already happened by the time a test
// runs; assigning to time.Local instead would make every parallel test in this
// package read a variable another test is writing, which is exactly what -race
// exists to catch. Handing the instant in keeps each case hermetic: the instant
// under test is stated in the case itself and is never the machine's.
func (s Settings) ValidateAt(now time.Time) error {
	return s.validateAt(now)
}
