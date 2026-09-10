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

// MaxSettingsFileBytes exposes readFile's size ceiling to the package's
// external test package, and only to it — this file is compiled into the test
// binary alone, so the shipped API does not grow to make the limit assertable.
//
// What it is for: writing a document exactly one byte over the limit. A test
// that spelled the number itself would keep passing after the constant moved,
// asserting a boundary that is no longer the boundary — and the boundary is the
// whole content of the rule.
const MaxSettingsFileBytes = maxSettingsFileBytes

// FileKind exposes fileKind so its branches can be tested by mode rather than
// by building one of each file type on disk.
//
// A socket and a device node are the reason this exists. A socket needs a
// listener, a character device needs a path like /dev/null that is not the
// test's to depend on, and a block device cannot be created without root — so
// exercising those branches through the filesystem would mean three skipped
// tests and one that cannot run at all. The function is a pure mapping from a
// mode to a phrase; testing it as one is both complete and honest about what it
// is.
//
// The same seam as internal/logging/export_test.go, for the same reason and
// over the near-copy of the same function; see fileKind for why the two are not
// shared.
var FileKind = fileKind
