// Package repofile reads and scaffolds the repo file, .smith/repo.yaml at a
// git repo's root: the loop settings committed with the repo so that everyone
// running the loop on it shares them.
package repofile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go.yaml.in/yaml/v3"

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
	// Labels name the triage labels the loop reads and writes on the tracker.
	Labels Labels `yaml:"labels"`
	// Prepare is the command that prepares a worktree of the repo, run by
	// sh -c in the worktree under its toolchain. Empty runs none.
	Prepare string `yaml:"-"`
}

// Labels are the names of the tracker's triage labels. A name left empty is
// unset, and the loop falls back to its built-in default for it.
type Labels struct {
	// Agent marks a task ready for an agent.
	Agent string `yaml:"agent"`
	// Human marks a task ready for a human, and is where a skipped task goes.
	Human string `yaml:"human"`
	// Spec marks an issue as a spec not yet split into tasks.
	Spec string `yaml:"spec"`
}

// document is the shape a repo file is decoded into: the File, with the
// prepare command taken as whatever node was written, so that its own check
// rather than the decoder reports a value of the wrong shape.
type document struct {
	File `yaml:",inline"`
	// Prepare is the node written under prepareKey.
	Prepare yaml.Node `yaml:"prepare"`
}

// Parse decodes a repo file strictly: any key it does not define, any label
// set to no name, and a prepare command that is not one non-empty string, is
// refused with its line, in a *blueprint.ValidationError. An empty document is
// valid and leaves every field unset.
func Parse(data []byte) (File, error) {
	var doc document
	if err := blueprint.DecodeStrict(data, subject, &doc); err != nil {
		return File{}, fmt.Errorf("parse repo file: %w", err)
	}
	if err := validationError(append(labelFindings(data), prepareFindings(data)...)); err != nil {
		return File{}, err
	}
	f := doc.File
	f.Prepare = resolved(&doc.Prepare).Value
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
