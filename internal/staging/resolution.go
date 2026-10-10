package staging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/hint"
)

// ResolutionPath is where the operator's resolved configuration is staged on
// the box. Like the document beside it, nothing in it is secret, so it is
// root-owned and world-readable: the smith user reads it and cannot change it.
const ResolutionPath = Root + "/" + resolutionFile

// The staged resolution's fixed shape. The writer and the on-box reader both
// derive their path from these, so the two cannot disagree about where it sits.
const (
	// resolutionFile is the staged resolution's name inside the box state
	// directory. It is JSON, like the staged env, because nothing edits it by
	// hand.
	resolutionFile = "resolved.json"
	// resolutionMode is the staged resolution's permissions, and
	// resolutionOwner its owner: the same as the document's, and for the same
	// reason.
	resolutionMode  = documentMode
	resolutionOwner = documentOwner
)

// Resolution is every fixed-key field the operator's machine resolved down the
// chain — flag, blueprint, preferences, default — except the provider, which a
// box never acts on. It holds values only: origins are `blueprint check`'s
// concern on the operator's machine, and the box acts on what was resolved.
type Resolution struct {
	// Access is how the box is reached.
	Access string `json:"access"`
	// Terminal is the terminal substrate sessions run under.
	Terminal string `json:"terminal"`
	// Workspace is the directory on the box the repos live under, as the
	// operator wrote it.
	Workspace string `json:"workspace"`
	// Git is the identity the box commits as, left out when nobody declared one.
	Git Identity `json:"git,omitzero"`
}

// Identity is the resolved git identity. A half nobody declared is empty, and
// is left out of the staged file rather than staged as the empty string.
type Identity struct {
	// UserName is the name commits are authored under.
	UserName string `json:"user_name,omitempty"`
	// UserEmail is the address commits are authored under.
	UserEmail string `json:"user_email,omitempty"`
}

// ResolutionFile is the resolved configuration as the staged tree carries it:
// the values, and the file they are staged in. The file's bytes stay empty
// until Resolve renders them, as the staged env's do.
type ResolutionFile struct {
	// Values are the resolved fixed-key fields.
	Values Resolution
	// File is where the values are staged.
	File File
}

// ResolutionPathIn is where the staged resolution sits under a box state
// directory. On-box smith reads ResolutionPathIn(Root); a test reads a
// directory of its own.
func ResolutionPathIn(root string) string {
	return filepath.Join(root, resolutionFile)
}

// LoadResolution reads the resolution staged under root, never falling back to a
// default. An absent file is an *AbsentError; an unparsable, incomplete or
// unrecognised one is a *MalformedError, either spelling its suggested command
// for inv.
func LoadResolution(root string, inv hint.Invocation) (Resolution, error) {
	path := ResolutionPathIn(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Resolution{}, &AbsentError{Path: path, Hint: inv}
		}
		return Resolution{}, fmt.Errorf("read staged resolution %s: %w", path, err)
	}
	var r Resolution
	if err := json.Unmarshal(data, &r); err != nil {
		return Resolution{}, &MalformedError{Path: path, Err: err, Hint: inv}
	}
	if err := r.validate(); err != nil {
		return Resolution{}, &MalformedError{Path: path, Err: err, Hint: inv}
	}
	return r, nil
}

// validate refuses a resolution the operator's machine could not have staged:
// one missing a field every resolution carries, or carrying a value outside
// the set smith recognises for it. A missing workspace matters most, since a
// repo path joined under an empty one lands in the working directory. The git
// identity is optional, whole or in part, because nobody has to declare one.
func (r Resolution) validate() error {
	var problems []error
	for _, f := range []struct{ name, value string }{
		{"access", r.Access}, {"terminal", r.Terminal}, {"workspace", r.Workspace},
	} {
		if f.value == "" {
			problems = append(problems, fmt.Errorf("%s is missing", f.name))
		}
	}
	if err := blueprint.ValidateAccess(r.Access); err != nil {
		problems = append(problems, fmt.Errorf("access: %w", err))
	}
	if err := blueprint.ValidateTerminal(r.Terminal); err != nil {
		problems = append(problems, fmt.Errorf("terminal: %w", err))
	}
	return errors.Join(problems...)
}

// plannedResolution is the resolved configuration as the staged tree carries
// it, planned on every run so a changed preference reaches the box on the next
// setup.
func plannedResolution(r Resolution) ResolutionFile {
	return ResolutionFile{
		Values: r,
		File:   File{Path: ResolutionPath, Mode: resolutionMode, Owner: resolutionOwner},
	}
}

// bytes renders the resolution as it is staged: JSON, indented, with a
// trailing newline, so an unchanged resolution renders to the same bytes and a
// re-run rewrites nothing.
func (r Resolution) bytes() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the resolved configuration: %w", err)
	}
	return append(data, '\n'), nil
}
