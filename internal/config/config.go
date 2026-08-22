// Package config owns smith's config home — the directory holding the
// operator's blueprints — and turns what the operator typed into a parsed
// blueprint.
//
// The home path is a dependency: it is passed in, never discovered here. That
// keeps the package free of any assumption about where an operator's home
// directory is, and lets a test run the real code path against a temp
// directory rather than a stand-in filesystem.
//
// Nothing in this package writes. Reading a config home that does not exist is
// reported, not repaired.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
)

// blueprintExt is the file extension every blueprint file carries. It is not
// part of a blueprint's name: the operator selects "acme", not "acme.yaml".
const blueprintExt = ".yaml"

// altBlueprintExt is the other spelling of the blueprint extension. smith
// writes ".yaml", but an operator who typed "acme.yml" made the same mistake
// and is owed the same correction.
const altBlueprintExt = ".yml"

// Home is a config home — the directory holding the operator's blueprints.
type Home struct {
	path string
}

// NewHome returns the config home rooted at path. The path is not read,
// checked, or created; a home that does not exist is a valid value whose reads
// fail when they happen.
func NewHome(path string) Home {
	return Home{path: path}
}

// Path returns the directory this config home is rooted at.
func (h Home) Path() string { return h.path }

// isPath reports whether what the operator typed is a path rather than a bare
// blueprint name. The two are told apart structurally: a value containing a
// separator, or beginning with "~" or ".", carries a path marker. Anything
// else is a name.
func isPath(nameOrPath string) bool {
	return strings.ContainsRune(nameOrPath, '/') ||
		strings.HasPrefix(nameOrPath, "~") ||
		strings.HasPrefix(nameOrPath, ".")
}

// Select resolves what the operator typed to the blueprint file it names. A
// value carrying a path marker is a path, used verbatim; a bare name resolves
// to <home>/blueprints/<name>.yaml.
func Select(home Home, nameOrPath string) (string, error) {
	if strings.TrimSpace(nameOrPath) == "" {
		return "", fmt.Errorf("no blueprint named: want a blueprint name such as %q", "acme")
	}
	if isPath(nameOrPath) {
		return nameOrPath, nil
	}
	// The extension is not part of the name, so accepting it here would make
	// "acme" and "acme.yaml" two spellings of one blueprint — an ambiguity the
	// marker's blueprint pointer would inherit.
	if ext := filepath.Ext(nameOrPath); ext == blueprintExt || ext == altBlueprintExt {
		bare := strings.TrimSuffix(nameOrPath, ext)
		return "", fmt.Errorf("blueprint %q: the %s extension is not part of a blueprint name — type %q instead", nameOrPath, ext, bare)
	}
	return filepath.Join(home.path, "blueprints", nameOrPath+blueprintExt), nil
}

// Load selects, reads, and parses the blueprint the operator named. A
// blueprint that does not exist is an error naming the path smith looked for,
// so the operator can see where it expected to find one.
func Load(home Home, nameOrPath string) (blueprint.Blueprint, error) {
	path, err := Select(home, nameOrPath)
	if err != nil {
		return blueprint.Blueprint{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator names their own blueprint.
	if err != nil {
		if os.IsNotExist(err) {
			if isPath(nameOrPath) {
				return blueprint.Blueprint{}, fmt.Errorf("no blueprint at %s", path)
			}
			return blueprint.Blueprint{}, fmt.Errorf("no blueprint named %q: looked for %s", nameOrPath, path)
		}
		return blueprint.Blueprint{}, fmt.Errorf("read blueprint %s: %w", path, err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		return blueprint.Blueprint{}, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// preferencesFile is the name of the operator-wide preferences file inside the
// config home.
const preferencesFile = "preferences" + blueprintExt

// PreferencesFile reports where the operator's preferences live and whether
// that file is there. Preferences are optional and so is the config home, so an
// absent file is a fact to report rather than an error; the caller falls
// through to smith's built-in defaults. Nothing is created.
func PreferencesFile(home Home) (path string, found bool, err error) {
	path = filepath.Join(home.path, preferencesFile)
	switch _, err := os.Stat(path); {
	case err == nil:
		return path, true, nil
	case os.IsNotExist(err):
		return path, false, nil
	default:
		return path, false, fmt.Errorf("look for preferences %s: %w", path, err)
	}
}

// LoadPreferences reads and parses the operator's preferences. Preferences are
// optional and so is the config home, so an absent file yields the zero
// Preferences and no error: smith falls through to its built-in defaults. An
// invalid one is an error naming the file, because preferences are part of
// what smith would use on every run and silently ignoring them would leave a
// box configured by something the operator never wrote.
func LoadPreferences(home Home) (blueprint.Preferences, error) {
	path, found, err := PreferencesFile(home)
	if err != nil {
		return blueprint.Preferences{}, err
	}
	if !found {
		return blueprint.Preferences{}, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the preferences live at a path smith derives itself.
	if err != nil {
		return blueprint.Preferences{}, fmt.Errorf("read preferences %s: %w", path, err)
	}
	p, err := blueprint.ParsePreferences(data)
	if err != nil {
		return blueprint.Preferences{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}
