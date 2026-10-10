// Package repofile reads and scaffolds the repo file, .smith/repo.yaml at a
// git repo's root: the loop settings committed with the repo so that everyone
// running the loop on it shares them.
package repofile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/byranZA/smith/internal/blueprint"
)

// subject is how a report about the repo file names it.
const subject = "repo file"

// File is the repo file's contents. A field left empty or nil is unset, and
// the loop falls back to its built-in default for it.
type File struct {
	// Agent names the agent adapter the loop hands each task to.
	Agent string `yaml:"agent"`
	// Model is the model the agent runs, passed to it verbatim.
	Model string `yaml:"model"`
	// Effort is how hard the agent thinks, on smith's own scale.
	Effort string `yaml:"effort"`
	// Push says whether the loop pushes the branch after each task the agent
	// closes. Nil is unset, which the loop takes as on.
	Push *bool `yaml:"push"`
}

// Parse decodes a repo file strictly: any key it does not define is refused
// with its line, in a *blueprint.ValidationError. An empty document is valid
// and leaves every field unset.
func Parse(data []byte) (File, error) {
	var f File
	if err := blueprint.DecodeStrict(data, subject, &f); err != nil {
		return File{}, fmt.Errorf("parse repo file: %w", err)
	}
	return f, nil
}

// Load reads and parses repo's repo file. The file is optional: a repo with
// none yields the empty File, which leaves every setting to its default.
func Load(repo Repo) (File, error) {
	data, err := os.ReadFile(repo.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read repo file: %w", err)
	}
	f, err := Parse(data)
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", repo.Path(), err)
	}
	return f, nil
}
