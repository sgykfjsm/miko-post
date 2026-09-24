package app

import (
	"fmt"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// LoadSettings resolves, reads and checks the settings one post will use
// (FR-005, FR-018, FR-053, FR-058).
//
// configured is the -c/--config value, or "" for the resolved default. The
// window always passes "", and has no way to pass anything else: FR-005 is kept
// by the window front door never being handed a path, not by this function
// telling callers apart.
//
// The path is returned alongside the error, and that is FR-030 rather than
// convenience: the startup-error window must show the resolved settings path,
// and the one moment the path matters most to a user is when the file at it is
// what is wrong. It is "" only when the default could not be resolved at all.
//
// Both front doors call this, so the order below is one sequence rather than
// two that can drift: resolve, read and validate, then refuse settings with no
// destination. Every failure returns before a sink, a service or a logger has
// been constructed, so no destination is ever started on settings that failed
// any of the three (FR-058), and "no post attempt" for FR-018 is a property of
// what exists when the error is returned.
//
// The no-destination error carries the path the same way config.Load's own
// errors do, so a user with two settings files is told which one is empty.
func LoadSettings(configured string) (path string, settings config.Settings, err error) {
	path = configured

	if path == "" {
		path, err = config.DefaultConfigPath()
		if err != nil {
			return "", config.Settings{}, err
		}
	}

	settings, err = config.Load(path)
	if err != nil {
		return path, config.Settings{}, err
	}

	if err := settings.RequireDestination(); err != nil {
		return path, config.Settings{}, fmt.Errorf("%s: %w", path, err)
	}

	return path, settings, nil
}
