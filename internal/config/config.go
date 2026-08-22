// Package config owns smith's config home — the directory holding the
// operator's blueprints — and turns what the operator typed into a parsed
// blueprint.
//
// The home path is a dependency: it is passed in, never discovered here. That
// keeps the package free of any assumption about where an operator's home
// directory is, and lets a test run the real code path against a temp
// directory rather than a stand-in filesystem.
//
// Reading never creates: a config home that does not exist is reported, not
// repaired. EnsureHome is the one thing here that writes, and only write paths
// call it — it creates the home and its cache, and appends to the operator's
// .gitignore rather than rewriting a file that is theirs.
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

// cacheDir is the gitignored directory inside the config home holding what
// smith derived rather than what the operator wrote. The box inventory is its
// only occupant.
const cacheDir = "cache"

// inventoryFile is the box inventory's name inside the cache directory.
const inventoryFile = "boxes.json"

// CachePath returns the config home's cache directory — the gitignored
// directory holding what smith derived rather than what the operator wrote.
// Nothing is read or created; the path is derived, not discovered.
func (h Home) CachePath() string { return filepath.Join(h.path, cacheDir) }

// InventoryPath returns the box inventory's file inside the config home's
// cache. Nothing is read or created, so a caller can name the file it would
// have read before any box exists.
func (h Home) InventoryPath() string { return filepath.Join(h.CachePath(), inventoryFile) }

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

// Document is a blueprint as smith read it: the parsed declaration, the bytes
// the file holds, and the file both came from.
//
// The bytes travel with the declaration because `machine setup` stages the
// operator's blueprint onto the box verbatim, and re-rendering it from the
// parsed declaration could only lose a field the operator wrote.
type Document struct {
	// Blueprint is the parsed declaration.
	Blueprint blueprint.Blueprint
	// Bytes is the blueprint file's content, exactly as it sits on disk.
	Bytes []byte
	// Path is the file the blueprint was read from.
	Path string
}

// LoadDocument selects, reads, and parses the blueprint the operator named,
// keeping its bytes verbatim alongside the declaration. A blueprint that does
// not exist is an error naming the path smith looked for, so the operator can
// see where it expected to find one.
func LoadDocument(home Home, nameOrPath string) (Document, error) {
	path, err := Select(home, nameOrPath)
	if err != nil {
		return Document{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator names their own blueprint.
	if err != nil {
		if os.IsNotExist(err) {
			if isPath(nameOrPath) {
				return Document{Path: path}, fmt.Errorf("no blueprint at %s", path)
			}
			return Document{Path: path}, fmt.Errorf("no blueprint named %q: looked for %s", nameOrPath, path)
		}
		return Document{Path: path}, fmt.Errorf("read blueprint %s: %w", path, err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		return Document{Path: path}, fmt.Errorf("%s: %w", path, err)
	}
	return Document{Blueprint: b, Bytes: data, Path: path}, nil
}

// Load selects, reads, and parses the blueprint the operator named, returning
// it alongside the file it came from. A blueprint that does not exist is an
// error naming the path smith looked for, so the operator can see where it
// expected to find one.
//
// The path comes back because a caller reporting on a blueprint has to name
// the file it read, and asking selection a second time to learn it would let
// the two answers drift apart.
func Load(home Home, nameOrPath string) (blueprint.Blueprint, string, error) {
	doc, err := LoadDocument(home, nameOrPath)
	if err != nil {
		return blueprint.Blueprint{}, doc.Path, err
	}
	return doc.Blueprint, doc.Path, nil
}

// preferencesFile is the name of the operator-wide preferences file inside the
// config home.
const preferencesFile = "preferences" + blueprintExt

// Preferences are the operator's preferences as smith read them: what they
// declare, the file they live in, and whether that file was there at all.
//
// The path and the fact of the file travel with the values because both are
// optional: a caller reporting on preferences has to be able to say "none at
// this path" as readily as "these, from this file", and neither can be told
// from the declared values alone.
type Preferences struct {
	// Declared is what the preferences file declares, or the zero value when
	// there is no such file.
	Declared blueprint.Preferences
	// Path is where the preferences live, whether or not a file is there.
	Path string
	// Found reports whether a preferences file was there to read.
	Found bool
}

// preferencesPath is where the operator's preferences live inside the config
// home. Nothing is read or created; the path is derived, not discovered.
func preferencesPath(home Home) string {
	return filepath.Join(home.path, preferencesFile)
}

// LoadPreferences reads and parses the operator's preferences, returning them
// alongside the file they came from. Preferences are optional and so is the
// config home, so an absent file yields unset preferences and no error, marked
// as not found: smith falls through to its built-in defaults. An invalid one
// is an error naming the file, because preferences are part of what smith
// would use on every run and silently ignoring them would leave a box
// configured by something the operator never wrote. Nothing is created.
func LoadPreferences(home Home) (Preferences, error) {
	path := preferencesPath(home)
	data, err := os.ReadFile(path) // #nosec G304 -- the preferences live at a path smith derives itself.
	if err != nil {
		if os.IsNotExist(err) {
			return Preferences{Path: path}, nil
		}
		return Preferences{Path: path}, fmt.Errorf("read preferences %s: %w", path, err)
	}
	p, err := blueprint.ParsePreferences(data)
	if err != nil {
		return Preferences{Path: path}, fmt.Errorf("%s: %w", path, err)
	}
	return Preferences{Declared: p, Path: path, Found: true}, nil
}
