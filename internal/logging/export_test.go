package logging

// FileKind exposes fileKind so its branches can be tested by mode rather than
// by building one of each file type on disk.
//
// A socket and a device node are the reason this exists. A socket needs a
// listener, a character device needs a path like /dev/null that is not the
// test's to depend on, and a block device cannot be created without root — so
// exercising those branches through the filesystem would mean three skipped
// tests and one that cannot run at all. The function is a pure mapping from a
// mode to a phrase; testing it as one is both complete and honest about what
// it is.
//
// Following internal/config/export_test.go, which uses the same pattern for
// the same reason.
var (
	FileKind    = fileKind
	UsableAsLog = usableAsLog
)
