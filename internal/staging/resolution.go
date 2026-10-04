package staging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// LoadResolution reads the resolution staged under root — /etc/smith on a real
// box. An absent file is refused as an *AbsentError and an unparsable one as a
// *MalformedError, both naming `machine setup`, exactly as the staged
// blueprint's refusals are. Nothing falls back to a default: a box silently
// acting on a default the operator did not resolve is the bug this file fixes.
func LoadResolution(root string) (Resolution, error) {
	path := ResolutionPathIn(root)
	data, err := os.ReadFile(path) // #nosec G304 -- the staged resolution sits at a path smith derives itself.
	if err != nil {
		if os.IsNotExist(err) {
			return Resolution{}, &AbsentError{Path: path}
		}
		return Resolution{}, fmt.Errorf("read staged resolution %s: %w", path, err)
	}
	var r Resolution
	if err := json.Unmarshal(data, &r); err != nil {
		return Resolution{}, &MalformedError{Path: path, Err: err}
	}
	return r, nil
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
