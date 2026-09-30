// Package starter scaffolds the operator's config home with starters: a
// commented preferences file and a commented blueprint, each valid as written
// and resolving to smith's built-in defaults until the operator uncomments
// something.
//
// The starters are embedded in the binary, because a binary-only install ships
// nothing else to copy from. They are the blank to fill; the worked examples in
// the docs are the filled-in reference, and neither is derived from the other.
//
// Scaffolding never overwrites. A file already in the config home is the
// operator's, so it is left byte-for-byte alone — not merged into, not updated
// — and only the starters that are missing are written.
package starter

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
)

// Blueprint is the name the starter blueprint is written under. It names a
// kind of box — the operator's own — rather than suggesting a default that is
// picked automatically, which no blueprint is.
const Blueprint = "personal"

// fileMode is the permission a starter is written with. A starter is where the
// operator will name their credentials, so it is no other account's business.
const fileMode fs.FileMode = 0o600

// dirMode is the permission the blueprints directory is created with, matching
// the config home it sits in.
const dirMode fs.FileMode = 0o700

//go:embed starters/preferences.yaml
var preferencesStarter []byte

//go:embed starters/personal.yaml
var blueprintStarter []byte

// Outcome is what scaffolding did with one starter: the file it names and
// whether the starter was written there or an existing file was left alone.
type Outcome struct {
	// Path is the file the starter belongs at.
	Path string
	// Created reports whether the starter was written. False means a file was
	// already there and was left untouched.
	Created bool
}

// Scaffold creates the config home at home, as the first write into it always
// does, then writes each starter whose file is absent. It returns one outcome
// per starter, preferences first, so the caller can report every file.
//
// identity is the git identity to declare in the starter preferences; it is
// not yet rendered in.
//
// A write that fails stops scaffolding with an error naming the path, since a
// half-scaffolded home is still one the operator can run init against again.
func Scaffold(home config.Home, identity blueprint.Git) ([]Outcome, error) {
	if err := config.EnsureHome(home); err != nil {
		return nil, fmt.Errorf("scaffold config home: %w", err)
	}
	blueprintPath, err := config.Select(home, Blueprint)
	if err != nil {
		return nil, fmt.Errorf("locate starter blueprint: %w", err)
	}
	starters := []struct {
		path    string
		content []byte
	}{
		{home.PreferencesPath(), preferencesStarter},
		{blueprintPath, blueprintStarter},
	}
	outcomes := make([]Outcome, 0, len(starters))
	for _, s := range starters {
		created, err := writeAbsent(s.path, s.content)
		if err != nil {
			return outcomes, err
		}
		outcomes = append(outcomes, Outcome{Path: s.path, Created: created})
	}
	return outcomes, nil
}

// writeAbsent writes content to path unless a file is already there, reporting
// whether it wrote. The existence check and the create are one exclusive open,
// so a file that appears between the two is still never overwritten.
func writeAbsent(path string, content []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode) // #nosec G304 -- the path is derived from the config home.
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(content); err != nil {
		return false, errors.Join(fmt.Errorf("write %s: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("close %s: %w", path, err)
	}
	return true, nil
}
