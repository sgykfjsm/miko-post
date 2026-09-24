package app_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
)

// TestLoadSettingsReturnsThePathOnEveryFailure is what FR-030's startup-error
// window depends on: the resolved path comes back with the error, so the window
// can show the file that is wrong. It also pins that a failure yields zero
// Settings, so no caller can post with what was half-read (FR-058).
//
// Not parallel: two cases set the variables path resolution reads.
func TestLoadSettingsReturnsThePathOnEveryFailure(t *testing.T) {
	dir := t.TempDir()

	write := func(t *testing.T, name, document string) string {
		t.Helper()

		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}

		return path
	}

	disabled := write(t, "disabled.toml", "[sink.obsidian]\nenabled = false\n[sink.telegram]\nenabled = false\n")
	invalid := write(t, "invalid.toml", "[logging]\nrotate_size_mib = 0\n")
	missing := filepath.Join(dir, "missing.toml")

	for _, tc := range []struct {
		name       string
		configured string
		is         error
	}{
		{name: "no destination enabled", configured: disabled, is: config.ErrNoDestinationEnabled},
		{name: "missing", configured: missing, is: fs.ErrNotExist},
		{name: "invalid", configured: invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, settings, err := app.LoadSettings(tc.configured)

			if err == nil {
				t.Fatal("LoadSettings accepted it")
			}

			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("err = %v, want it to match %v", err, tc.is)
			}

			if path != tc.configured {
				t.Errorf("path = %q, want %q", path, tc.configured)
			}

			if settings != (config.Settings{}) {
				t.Errorf("a failure returned settings: %+v", settings)
			}
		})
	}

	t.Run("the default is resolved when nothing was configured", func(t *testing.T) {
		configHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", configHome)

		path, _, err := app.LoadSettings("")

		if want := filepath.Join(configHome, "miko-post", "config.toml"); path != want {
			t.Errorf("path = %q, want the resolved default %q", path, want)
		}

		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want the missing default reported", err)
		}
	})

	t.Run("an unresolvable default has no path", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("XDG_CONFIG_HOME", "")

		path, _, err := app.LoadSettings("")

		if err == nil || path != "" {
			t.Errorf("LoadSettings(\"\") = %q, %v; want no path and an error", path, err)
		}
	})
}
