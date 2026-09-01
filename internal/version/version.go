// Package version exposes the application version and git commit recorded in
// diagnostic records.
//
// Values are stamped by the linker for local and release builds. Because
// `go install` — the documented v0.1 distribution channel — cannot pass
// linker flags, unstamped builds fall back to the module version and VCS
// revision that the toolchain records in the binary's build info.
package version

import "runtime/debug"

// Stamped by the linker, e.g.
//
//	go build -ldflags "-X github.com/sgykfjsm/miko-post/internal/version.version=0.1.0"
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

// fromBuildInfo reads the module version and VCS revision the toolchain embeds.
// A module built from a local directory reports the placeholder "(devel)", which
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

	return version, commit
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
