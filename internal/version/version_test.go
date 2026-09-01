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
			name:          "missing vcs revision yields unknown commit",
			readBuildInfo: buildInfo("v0.1.0"),
			wantVersion:   "v0.1.0",
			wantCommit:    Unknown,
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
