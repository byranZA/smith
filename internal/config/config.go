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

// Select resolves what the operator typed to the blueprint file it names. A
// bare name resolves to <home>/blueprints/<name>.yaml.
func Select(home Home, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("no blueprint named: want a blueprint name such as %q", "acme")
	}
	return filepath.Join(home.path, "blueprints", name+blueprintExt), nil
}

// Load selects, reads, and parses the blueprint the operator named. A
// blueprint that does not exist is an error naming the path smith looked for,
// so the operator can see where it expected to find one.
func Load(home Home, name string) (blueprint.Blueprint, error) {
	path, err := Select(home, name)
	if err != nil {
		return blueprint.Blueprint{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator names their own blueprint.
	if err != nil {
		if os.IsNotExist(err) {
			return blueprint.Blueprint{}, fmt.Errorf("no blueprint named %q: looked for %s", name, path)
		}
		return blueprint.Blueprint{}, fmt.Errorf("read blueprint %s: %w", path, err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		return blueprint.Blueprint{}, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}
