package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Directory and file names for the two resolved paths.
//
// appDirName is shared by both so a user's configuration and state land under
// the same name in two different roots.
const (
	appDirName     = "miko-post"
	configFileName = "config.toml"
	logFileName    = "app.jsonl"
)

// The two environment variables this version reads for path resolution.
//
// They are named here rather than inlined so the help output (T080) and the
// tests refer to the same constants the resolution does.
const (
	xdgConfigHomeEnv = "XDG_CONFIG_HOME"
	xdgStateHomeEnv  = "XDG_STATE_HOME"
)

// DefaultConfigPath resolves the settings file the window always uses and that
// CLI posting uses when -c is absent (FR-053, contracts/config-schema.md).
//
//	$XDG_CONFIG_HOME/miko-post/config.toml
//	~/.config/miko-post/config.toml      when the variable is unset or empty
//
// The resolved value appears verbatim in --help (FR-007, T080), so it is a
// user-visible string and not only an internal lookup.
func DefaultConfigPath() (string, error) {
	base, err := xdgBase(xdgConfigHomeEnv, ".config")
	if err != nil {
		return "", err
	}

	return filepath.Join(base, appDirName, configFileName), nil
}

// DefaultLogPath resolves the diagnostic log path used when logging.path is
// empty (FR-056, FR-065).
//
//	$XDG_STATE_HOME/miko-post/app.jsonl
//	~/.local/state/miko-post/app.jsonl   when the variable is unset or empty
//
// This returns a path only. Creating the directory, and failing soft when that
// is impossible, belongs to the logger (T023, FR-075) — a settings load must
// not touch the filesystem on the user's behalf.
func DefaultLogPath() (string, error) {
	base, err := xdgBase(xdgStateHomeEnv, ".local", "state")
	if err != nil {
		return "", err
	}

	return filepath.Join(base, appDirName, logFileName), nil
}

// xdgBase returns the value of env, or the home directory joined with fallback
// when env is unset or set to the empty string.
//
// Treating unset and empty identically is required by FR-053 and FR-065 and is
// not the same as os.Getenv returning "" for both: the distinction is invisible
// through Getenv, so this uses LookupEnv and then tests the value explicitly.
// That makes the empty case a deliberate branch rather than an accident of the
// API, which is the difference the two requirements are actually about — an
// `export XDG_CONFIG_HOME=` in a shell profile is a common way to end up here.
//
// The XDG base-directory specification additionally says a relative path should
// be treated as invalid and ignored. That rule is deliberately not implemented:
// FR-053, FR-065 and contracts/config-schema.md each enumerate exactly two
// conditions — unset and empty — and adding a third would be behaviour this
// version's contract does not describe. A relative XDG_CONFIG_HOME therefore
// yields a relative settings path, which resolves against the working
// directory. Recorded here so the omission reads as a decision rather than an
// oversight.
//
// A failure to determine the home directory is returned rather than swallowed.
// os.UserHomeDir fails only when $HOME is unset on Unix, and in that state no
// invented path would be right.
func xdgBase(env string, fallback ...string) (string, error) {
	if value, ok := os.LookupEnv(env); ok && value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for the %s fallback: %w", env, err)
	}

	return filepath.Join(append([]string{home}, fallback...)...), nil
}
