package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// TestDefaultPaths covers FR-053 and FR-065 together, because they are the same
// rule applied to two variables and the interesting case is shared: a variable
// that is set but empty must behave exactly like one that is unset.
//
// That case is the whole point of the test. os.Getenv cannot tell the two
// apart, so an implementation written with Getenv passes the unset case and the
// set case and silently produces "/miko-post/config.toml" for the empty one —
// an absolute path in the filesystem root. The tests are not parallel because
// they set process-wide environment variables through t.Setenv.
func TestDefaultPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	tests := []struct {
		name     string
		variable string
		// unset and env are separate because the distinction between them is
		// exactly what is under test: unset removes the variable, env == ""
		// with unset false leaves it present and empty.
		unset   bool
		env     string
		resolve func() (string, error)
		want    string
	}{
		{
			name:     "config: XDG_CONFIG_HOME set",
			variable: "XDG_CONFIG_HOME",
			env:      "/xdg/config",
			resolve:  config.DefaultConfigPath,
			want:     filepath.Join("/xdg/config", "miko-post", "config.toml"),
		},
		{
			name:     "config: XDG_CONFIG_HOME unset",
			variable: "XDG_CONFIG_HOME",
			unset:    true,
			resolve:  config.DefaultConfigPath,
			want:     filepath.Join(home, ".config", "miko-post", "config.toml"),
		},
		{
			name:     "config: XDG_CONFIG_HOME set but empty",
			variable: "XDG_CONFIG_HOME",
			env:      "",
			resolve:  config.DefaultConfigPath,
			want:     filepath.Join(home, ".config", "miko-post", "config.toml"),
		},
		{
			name:     "log: XDG_STATE_HOME set",
			variable: "XDG_STATE_HOME",
			env:      "/xdg/state",
			resolve:  config.DefaultLogPath,
			want:     filepath.Join("/xdg/state", "miko-post", "app.jsonl"),
		},
		{
			name:     "log: XDG_STATE_HOME unset",
			variable: "XDG_STATE_HOME",
			unset:    true,
			resolve:  config.DefaultLogPath,
			want:     filepath.Join(home, ".local", "state", "miko-post", "app.jsonl"),
		},
		{
			name:     "log: XDG_STATE_HOME set but empty",
			variable: "XDG_STATE_HOME",
			env:      "",
			resolve:  config.DefaultLogPath,
			want:     filepath.Join(home, ".local", "state", "miko-post", "app.jsonl"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// t.Setenv first in both branches: it records the original value
			// and registers the cleanup that restores it, so the subsequent
			// Unsetenv is still undone when the test ends.
			t.Setenv(test.variable, test.env)

			if test.unset {
				if err := os.Unsetenv(test.variable); err != nil {
					t.Fatalf("os.Unsetenv(%s): %v", test.variable, err)
				}
			}

			got, err := test.resolve()
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}

			if got != test.want {
				t.Errorf("resolved %q, want %q", got, test.want)
			}
		})
	}
}

// TestDefaultPathsAreDistinct guards the copy-paste failure the two resolvers
// invite: they differ only in a variable name, a fallback, and a filename, so a
// wrong one produces a plausible path in the right shape.
func TestDefaultPathsAreDistinct(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
	t.Setenv("XDG_STATE_HOME", "/xdg/state")

	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}

	logPath, err := config.DefaultLogPath()
	if err != nil {
		t.Fatalf("DefaultLogPath: %v", err)
	}

	if configPath == logPath {
		t.Fatalf("the config and log paths resolved to the same value: %q", configPath)
	}

	if filepath.Ext(configPath) != ".toml" {
		t.Errorf("config path %q should end in .toml", configPath)
	}

	if filepath.Ext(logPath) != ".jsonl" {
		t.Errorf("log path %q should end in .jsonl", logPath)
	}
}

// TestDefaultPathsWithoutAHome covers the branch where the fallback itself
// cannot be built.
//
// os.UserHomeDir fails on Unix when $HOME is empty, and the resolvers return
// that failure rather than inventing a path. The alternative — falling back to
// the working directory, or to "/" — would write a user's notes configuration
// and diagnostic log somewhere arbitrary and then look like it had worked.
func TestDefaultPathsWithoutAHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir reads USERPROFILE on Windows, not HOME")
	}

	t.Setenv("HOME", "")

	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		t.Setenv(variable, "")

		if err := os.Unsetenv(variable); err != nil {
			t.Fatalf("os.Unsetenv(%s): %v", variable, err)
		}
	}

	for name, resolve := range map[string]func() (string, error){
		"DefaultConfigPath": config.DefaultConfigPath,
		"DefaultLogPath":    config.DefaultLogPath,
	} {
		path, err := resolve()
		if err == nil {
			t.Errorf("%s returned %q with no home directory, want an error", name, path)
		}

		if path != "" {
			t.Errorf("%s returned %q alongside an error, want the empty string", name, path)
		}
	}
}
