package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/cli"
)

// helpFor renders the help text contracts/cli-interface.md specifies, with
// defaultPath in its one variable position.
//
// Written out here rather than reusing the production format string, so the
// assertion is against the contract's text and a typo in help.go is a failure
// rather than a change both sides agree on.
func helpFor(defaultPath string) string {
	return "Usage:\n" +
		"  mp [options] [message...]\n" +
		"\n" +
		"If no message is specified, the GUI is launched.\n" +
		"\n" +
		"Options:\n" +
		"  -c, --config PATH\n" +
		"        Path to the configuration file for CLI posting.\n" +
		"        Default: " + defaultPath + "\n" +
		"\n" +
		"  -h, --help\n" +
		"        Show this help.\n"
}

// TestHelpPrintsTheResolvedDefaultPath is T075 and FR-007: help carries the
// settings path resolved for the current environment, never an unexpanded
// $XDG_CONFIG_HOME expression, and exits 0.
//
// Not parallel: each case sets the two variables resolution reads.
func TestHelpPrintsTheResolvedDefaultPath(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "xdg")

	tests := []struct {
		name string
		xdg  string
		want string
	}{
		{
			name: "XDG_CONFIG_HOME set",
			xdg:  xdg,
			want: filepath.Join(xdg, "miko-post", "config.toml"),
		},
		{
			// The empty case is the one FR-053 singles out, and the one where a
			// literal expression would be most misleading: it would expand to a
			// path under the filesystem root.
			name: "XDG_CONFIG_HOME set but empty",
			xdg:  "",
			want: filepath.Join(home, ".config", "miko-post", "config.toml"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", tt.xdg)

			var out bytes.Buffer

			if status := cli.Help(&out); status != cli.ExitSuccess {
				t.Errorf("Help returned %d, want %d", status, cli.ExitSuccess)
			}

			if got := out.String(); got != helpFor(tt.want) {
				t.Errorf("help output:\n%s\nwant:\n%s", got, helpFor(tt.want))
			}

			if strings.Contains(out.String(), "$") {
				t.Errorf("help contains an unexpanded variable: %q", out.String())
			}
		})
	}
}

// TestHelpStillHelpsWhenTheDefaultCannotBeResolved: with neither $HOME nor
// XDG_CONFIG_HOME there is no default path, and help still prints — with the
// reason in the path's place — and still exits 0, because that user needs the
// -c option more than anyone.
func TestHelpStillHelpsWhenTheDefaultCannotBeResolved(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	var out bytes.Buffer

	if status := cli.Help(&out); status != cli.ExitSuccess {
		t.Errorf("Help returned %d, want %d", status, cli.ExitSuccess)
	}

	got := out.String()

	if !strings.Contains(got, "-c, --config PATH") {
		t.Errorf("help lost the option list when the default was unresolvable:\n%s", got)
	}

	if !strings.Contains(got, "Default: could not be resolved (") {
		t.Errorf("help does not say the default could not be resolved:\n%s", got)
	}
}
