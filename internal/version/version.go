// Package version exposes the application version and git commit recorded in
// diagnostic records.
//
// Values are stamped by the linker for local and release builds. An end user
// installing with `go install <path>@<version>` passes no linker flags, so
// unstamped builds fall back to the build info the toolchain embeds in the
// binary.
//
// That fallback is weaker than it looks, which is why it is spelled out here.
// A build from a git working tree records vcs.revision, so the commit is exact.
// A build from the module cache — which is what `go install <path>@<version>`
// produces — records no vcs information at all. For an untagged install the
// module version is a pseudo-version whose final field is the 12-character
// commit prefix, so the commit is still recoverable; for a tagged install
// nothing identifies the commit and Commit reports Unknown.
package version

import (
	"runtime/debug"
	"strings"
)

// Stamped by the linker, e.g.
//
//	go build -ldflags "-X github.com/sgykfjsm/miko-post/internal/version.version=0.1.0"
//
// Prefer `make build`, which assembles these flags with the correct paths.
var (
	version string
	commit  string
)

// Unknown is reported when neither the linker nor the build info supplies a value.
const Unknown = "unknown"

var resolved = resolve(version, commit, debug.ReadBuildInfo)

// Version reports the application version.
func Version() string { return resolved.version }

// Commit reports the git commit the binary was built from.
func Commit() string { return resolved.commit }

type info struct {
	version string
	commit  string
}

// resolve applies the precedence rule: a linker-stamped value wins, then the
// build info recorded by the toolchain, then Unknown. It takes readBuildInfo as
// a parameter so the precedence is testable without rebuilding the binary.
func resolve(version, commit string, readBuildInfo func() (*debug.BuildInfo, bool)) info {
	buildVersion, buildCommit := fromBuildInfo(readBuildInfo)

	return info{
		version: firstNonEmpty(version, buildVersion, Unknown),
		commit:  firstNonEmpty(commit, buildCommit, Unknown),
	}
}

// fromBuildInfo reads the module version and commit the toolchain embeds. A
// module built from a local directory reports the placeholder "(devel)", which
// is no more informative than Unknown, so it is discarded.
func fromBuildInfo(readBuildInfo func() (*debug.BuildInfo, bool)) (version, commit string) {
	buildInfo, ok := readBuildInfo()
	if !ok {
		return "", ""
	}

	if v := buildInfo.Main.Version; v != "" && v != "(devel)" {
		version = v
	}

	for _, setting := range buildInfo.Settings {
		if setting.Key == "vcs.revision" {
			commit = setting.Value
			break
		}
	}

	// A module-cache build records no vcs information, so recover the commit
	// from the pseudo-version when the module version is one.
	if commit == "" {
		commit = commitFromPseudoVersion(version)
	}

	return version, commit
}

// commitFromPseudoVersion extracts the commit prefix from a Go pseudo-version,
// whose last two hyphen-separated fields are a 14-digit UTC timestamp and a
// 12-character commit prefix. It returns "" for a released version like v0.1.0,
// which identifies no commit.
//
// All three pseudo-version forms are accepted, which differ in how the
// timestamp field is prefixed:
//
//	v0.0.0-20260901063448-50c860568dd2           // no base version
//	v1.2.4-0.20260901063448-50c860568dd2         // base is a release
//	v1.2.3-rc.1.0.20260901063448-50c860568dd2    // base is a pre-release
func commitFromPseudoVersion(moduleVersion string) string {
	// Build metadata such as the toolchain's "+dirty" marker is not part of the
	// pseudo-version grammar.
	if plus := strings.IndexByte(moduleVersion, '+'); plus >= 0 {
		moduleVersion = moduleVersion[:plus]
	}

	// Splitting on "-" would destroy the information this check needs: once the
	// string is fields, a "0." marker preceded by "-" is indistinguishable from
	// one preceded by ".", and only the latter is a pseudo-version. So the
	// prerelease is kept intact and the marker must be a whole dot-separated
	// component, mirroring Go's ([^+]*\.)?0\. grammar.
	dash := strings.IndexByte(moduleVersion, '-')
	if dash < 0 {
		return ""
	}

	_, minor, patch, ok := semverCore(moduleVersion[:dash])
	if !ok {
		return ""
	}

	// The revision is everything after the final "-"; the base prerelease may
	// itself contain hyphens, so anchor on the last one.
	prerelease := moduleVersion[dash+1:]

	lastDash := strings.LastIndexByte(prerelease, '-')
	if lastDash < 0 {
		return ""
	}

	stamp, revision := prerelease[:lastDash], prerelease[lastDash+1:]

	// Go's grammar allows any [A-Za-z0-9]+ revision, but a git module always
	// yields a 12-character lowercase-hex prefix. Narrowing to that fails
	// closed: an unrecognized shape reports no commit rather than a wrong one.
	if !isLowerHex(revision, 12) || len(stamp) < 14 {
		return ""
	}

	prefix, timestamp := stamp[:len(stamp)-14], stamp[len(stamp)-14:]
	if !isDigits(timestamp, 14) {
		return ""
	}

	switch {
	case prefix == "":
		// No-base form, which Go writes only as vX.0.0-<timestamp>-<revision>.
		if minor != "0" || patch != "0" {
			return ""
		}
	case prefix == "0." || strings.HasSuffix(prefix, ".0."):
		// Base-version forms: vX.Y.Z-0.<ts>-<rev> and vX.Y.Z-<pre>.0.<ts>-<rev>.
	default:
		return ""
	}

	return revision
}

// semverCore splits a "vMAJOR.MINOR.PATCH" version core, which is what precedes
// a pseudo-version's first hyphen.
func semverCore(s string) (major, minor, patch string, ok bool) {
	if !strings.HasPrefix(s, "v") {
		return "", "", "", false
	}

	parts := strings.Split(s[1:], ".")
	if len(parts) != 3 {
		return "", "", "", false
	}

	for _, part := range parts {
		// Semver forbids a leading zero on a numeric identifier, so "v01.0.0" is
		// not a version core and must not be treated as one.
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return "", "", "", false
		}

		for _, r := range part {
			if r < '0' || r > '9' {
				return "", "", "", false
			}
		}
	}

	return parts[0], parts[1], parts[2], true
}

func isDigits(s string, length int) bool {
	if len(s) != length {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func isLowerHex(s string, length int) bool {
	if len(s) != length {
		return false
	}

	for _, r := range s {
		isDigit := r >= '0' && r <= '9'
		isHexLetter := r >= 'a' && r <= 'f'

		if !isDigit && !isHexLetter {
			return false
		}
	}

	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
