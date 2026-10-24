package workbench

import (
	"errors"
	"path/filepath"
)

// ErrNoStateHome is why StateHome cannot locate the state home: neither an
// absolute $XDG_STATE_HOME nor $HOME is set.
var ErrNoStateHome = errors.New("locate the state home: set $HOME, or $XDG_STATE_HOME to an absolute path")

// StateHome returns the state home, smith's working state on this machine, as
// getenv's environment places it: $XDG_STATE_HOME/smith when that is an
// absolute path, else ~/.local/state/smith, on every OS.
func StateHome(getenv func(string) string) (string, error) {
	if xdg := getenv("XDG_STATE_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "smith"), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", ErrNoStateHome
	}
	return filepath.Join(home, ".local", "state", "smith"), nil
}
