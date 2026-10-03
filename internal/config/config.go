// Package config owns smith's config home — the directory holding the
// operator's blueprints, passed in rather than discovered — and turns what the
// operator typed into a parsed blueprint. Reading never creates; EnsureHome is
// the one thing here that writes.
package config

import (
	"fmt"
	"io/fs"
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
// blueprint name. A value containing a separator, or beginning with "~" or
// ".", carries a path marker; anything else is a name.
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
	if ext, ok := blueprintExtOf(nameOrPath); ok {
		bare := strings.TrimSuffix(nameOrPath, ext)
		return "", fmt.Errorf("blueprint %q: the %s extension is not part of a blueprint name — type %q instead", nameOrPath, ext, bare)
	}
	return filepath.Join(home.path, "blueprints", nameOrPath+blueprintExt), nil
}

// blueprintExtOf returns the blueprint extension a bare name carries, and
// whether it carries one. Select refuses such a name, so "acme" and
// "acme.yaml" never become two spellings of one blueprint.
func blueprintExtOf(name string) (string, bool) {
	ext := filepath.Ext(name)
	return ext, ext == blueprintExt || ext == altBlueprintExt
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if isPath(nameOrPath) {
				return Document{Path: path}, fmt.Errorf("no blueprint at %s: %w", path, fs.ErrNotExist)
			}
			return Document{Path: path}, fmt.Errorf("no blueprint named %q: looked for %s: %w", nameOrPath, path, fs.ErrNotExist)
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
// it alongside the file it came from, even on error. A blueprint that does not
// exist is an error naming the path smith looked for.
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

// PreferencesPath returns where the operator's preferences live inside the
// config home. Nothing is read or created; the path is derived, not discovered.
func (h Home) PreferencesPath() string {
	return filepath.Join(h.path, preferencesFile)
}

// LoadPreferences reads and parses the operator's preferences without
// creating anything, returning them alongside the file they came from. An
// absent file yields unset preferences marked not found and no error; an
// invalid one is an error naming the file.
func LoadPreferences(home Home) (Preferences, error) {
	path := home.PreferencesPath()
	data, err := os.ReadFile(path)
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
