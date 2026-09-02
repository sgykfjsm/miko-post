package version

import (
	"runtime/debug"
	"testing"
)

func buildInfo(mainVersion string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main:     debug.Module{Version: mainVersion},
			Settings: settings,
		}, true
	}
}

func noBuildInfo() (*debug.BuildInfo, bool) { return nil, false }

func TestResolve(t *testing.T) {
	revision := debug.BuildSetting{Key: "vcs.revision", Value: "abc1234"}

	tests := []struct {
		name            string
		version, commit string
		readBuildInfo   func() (*debug.BuildInfo, bool)
		wantVersion     string
		wantCommit      string
	}{
		{
			name:          "linker stamp wins over build info",
			version:       "0.1.0",
			commit:        "deadbee",
			readBuildInfo: buildInfo("v9.9.9", revision),
			wantVersion:   "0.1.0",
			wantCommit:    "deadbee",
		},
		{
			name:          "falls back to build info when unstamped",
			readBuildInfo: buildInfo("v0.1.0", revision),
			wantVersion:   "v0.1.0",
			wantCommit:    "abc1234",
		},
		{
			name:          "unknown when build info is unavailable",
			readBuildInfo: noBuildInfo,
			wantVersion:   Unknown,
			wantCommit:    Unknown,
		},
		{
			name:          "devel placeholder is discarded",
			readBuildInfo: buildInfo("(devel)", revision),
			wantVersion:   Unknown,
			wantCommit:    "abc1234",
		},
		{
			// A tagged module-cache install: no vcs.revision, and a released
			// version identifies no commit. This is the shape `go install
			// <path>@v0.1.0` actually produces.
			name:          "tagged module install yields unknown commit",
			readBuildInfo: buildInfo("v0.1.0"),
			wantVersion:   "v0.1.0",
			wantCommit:    Unknown,
		},
		{
			// An untagged module-cache install: no vcs.revision either, but the
			// pseudo-version carries the commit prefix. This is `go install
			// <path>@latest` against an untagged default branch.
			name:          "untagged module install recovers commit from pseudo-version",
			readBuildInfo: buildInfo("v0.0.0-20260901063448-50c860568dd2"),
			wantVersion:   "v0.0.0-20260901063448-50c860568dd2",
			wantCommit:    "50c860568dd2",
		},
		{
			name:          "vcs revision wins over the pseudo-version prefix",
			readBuildInfo: buildInfo("v0.0.0-20260901063448-50c860568dd2", revision),
			wantVersion:   "v0.0.0-20260901063448-50c860568dd2",
			wantCommit:    "abc1234",
		},
		{
			name:          "each field falls back independently",
			commit:        "deadbee",
			readBuildInfo: buildInfo("v0.1.0", revision),
			wantVersion:   "v0.1.0",
			wantCommit:    "deadbee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolve(tt.version, tt.commit, tt.readBuildInfo)

			if got.version != tt.wantVersion {
				t.Errorf("version = %q, want %q", got.version, tt.wantVersion)
			}
			if got.commit != tt.wantCommit {
				t.Errorf("commit = %q, want %q", got.commit, tt.wantCommit)
			}
		})
	}
}

// Version and Commit must never return an empty string: a diagnostic record
// with an empty version field is indistinguishable from a missing field.
func TestExportedAccessorsAreNeverEmpty(t *testing.T) {
	if Version() == "" {
		t.Error("Version() is empty")
	}
	if Commit() == "" {
		t.Error("Commit() is empty")
	}
}

func TestCommitFromPseudoVersion(t *testing.T) {
	tests := []struct {
		name          string
		moduleVersion string
		want          string
	}{
		{"canonical pseudo-version", "v0.0.0-20260901063448-50c860568dd2", "50c860568dd2"},
		{"release base prefix", "v1.2.4-0.20260901063448-50c860568dd2", "50c860568dd2"},
		{"pre-release base prefix", "v1.2.3-rc.1.0.20260901063448-50c860568dd2", "50c860568dd2"},
		{"dirty build metadata is stripped", "v0.0.0-20260901063448-50c860568dd2+dirty", "50c860568dd2"},
		{"incompatible suffix is stripped", "v2.0.1-0.20260901063448-50c860568dd2+incompatible", "50c860568dd2"},
		{"released tag identifies no commit", "v0.1.0", ""},
		{"pre-release tag is not a pseudo-version", "v0.1.0-rc.1", ""},
		{"devel placeholder", "(devel)", ""},
		{"empty", "", ""},
		{"short revision field is rejected", "v0.0.0-20260901063448-50c860568d", ""},
		{"non-hex revision field is rejected", "v0.0.0-20260901063448-50c860568dzz", ""},
		{"uppercase revision field is rejected", "v0.0.0-20260901063448-50C860568DD2", ""},
		{"non-numeric timestamp is rejected", "v0.0.0-2026090106344X-50c860568dd2", ""},
		// Grammar, not merely shape. Each of these has a 14-digit field and a
		// 12-char lowercase-hex field in the last two positions, so a shape-only
		// check would report a commit that does not exist (COR-102 / ADV-101).
		{"prerelease base without the 0 marker is rejected", "v1.0.0-alpha.20260901063448-deadbeef1234", ""},
		{"prerelease base with a non-zero marker is rejected", "v1.0.0-rc.1.20260901063448-deadbeef1234", ""},
		{"bare timestamp with extra fields is rejected", "v1.0.0-rc1-20260901063448-deadbeefcafe", ""},
		{"no-base form requires vX.0.0", "v1.2.3-20260901063448-50c860568dd2", ""},
		{"no-base form with a non-zero major is valid", "v1.0.0-20260901063448-50c860568dd2", "50c860568dd2"},
		// A hyphen before the "0." marker is legal semver but is NOT a
		// pseudo-version, so it must not yield a commit (COR-201).
		{"hyphen before the 0 marker is rejected", "v1.2.3-alpha-0.20260901063448-deadbeefcafe", ""},
		{"hyphen before the 0 marker, numeric pre", "v1.2.3-rc1-0.20260901063448-deadbeefcafe", ""},
		// A hyphen *inside* the base prerelease is legal and must still work.
		{"hyphenated base prerelease is valid", "v1.2.3-alpha-beta.0.20260901063448-deadbeefcafe", "deadbeefcafe"},
		{"incompatible metadata is stripped", "v2.0.1-0.20260901063448-50c860568dd2+incompatible", "50c860568dd2"},
		// Guards that mutation testing showed were unpinned (COR-204).
		{"empty core component is rejected", "v1..0-20260901063448-abcdefabcdef", ""},
		{"four-component core is rejected", "v1.0.0.0-20260901063448-abcdefabcdef", ""},
		{"two-component core is rejected", "v1.0-0.20260901063448-abcdefabcdef", ""},
		{"missing revision separator is rejected", "v0.0.0-20260901063448", ""},
		// Further members of the hyphen-attached marker family (ADV-201).
		{"dotted pre with hyphen-attached marker", "v1.0.0-alpha.1-0.20260901063448-deadbeefcafe", ""},
		{"multi-hyphen pre with hyphen-attached marker", "v2.3.4-build-7-0.20260901063448-deadbeefcafe", ""},
		{"numeric pre with hyphen-attached marker", "v1.0.0-0-0.20260901063448-deadbeefcafe", ""},
		{"leading zero in the core is rejected", "v01.0.0-20260901063448-deadbeefcafe", ""},
		{"non-version base is rejected", "garbage-20260901063448-50c860568dd2", ""},
		{"empty base is rejected", "-20260901063448-50c860568dd2", ""},
		{"base without a v prefix is rejected", "1.0.0-20260901063448-50c860568dd2", ""},
		{"base with two components is rejected", "v1.0-20260901063448-50c860568dd2", ""},
		{"base with a non-numeric component is rejected", "vX.0.0-20260901063448-50c860568dd2", ""},
		{"short timestamp is rejected", "v0.0.0-2026090163448-50c860568dd2", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commitFromPseudoVersion(tt.moduleVersion); got != tt.want {
				t.Errorf("commitFromPseudoVersion(%q) = %q, want %q", tt.moduleVersion, got, tt.want)
			}
		})
	}
}
