package staging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EnvPath is where the blueprint's env is staged on the box, with every value
// already resolved. It is smith-owned and 0600, unlike the world-readable
// document beside it: these are provisioned secrets, and the account that
// consumes them is the only one owed a read.
const EnvPath = Root + "/" + envFile

// The staged env's fixed shape. The writer and the on-box reader both derive
// their path from these, so the two cannot disagree about where the values sit.
const (
	// envFile is the staged env's name inside the box state directory. It is
	// JSON rather than the document's YAML because nothing edits it by hand:
	// smith writes it whole and reads it back.
	envFile = "env.json"
	// envMode is the staged env's permissions, and envOwner its owner. A
	// resolved value is a provisioned secret.
	envMode  = defaultPerms
	envOwner = smithOwner
)

// Env is a blueprint's env by the scope it was declared at: the box's own
// variables, and each repo's overrides keyed by repo name.
//
// It carries references before they are resolved and values afterwards, and
// which one it holds is decided by where it sits — the tree's references
// before Resolve, the staged file's values after. Only the operator's machine
// ever turns one into the other.
type Env struct {
	// Box are the variables the blueprint declares at top level, keyed by
	// variable name.
	Box map[string]string `json:"box,omitempty"`
	// Repos are the variables each repo declares, keyed by repo name and then
	// by variable name.
	Repos map[string]map[string]string `json:"repos,omitempty"`
}

// EnvFile is the blueprint's env as the staged tree carries it: the references
// it declares, and the file their resolved values are staged in.
//
// The references resolve only on the operator's machine — env:GH_TOKEN names a
// variable in the operator's shell and a VPS re-reading it would export
// nothing — so the file's bytes stay empty until Resolve fills them, exactly as
// a placement's do.
type EnvFile struct {
	// References are the values as the blueprint declares them, by scope.
	// They are carried for reporting a reference that will not resolve, and
	// are never read on the box.
	References Env
	// File is where the resolved values are staged.
	File File
}

// MissingValueError reports a variable the staged blueprint declares that has
// no staged value beside it. `machine setup` writes the document and the values
// in one run, so the two only come apart when /etc/smith/ has been edited by
// hand or the box was staged by a smith that predates the artifact — and the
// value cannot be recovered from the box, because the reference it came from
// resolves only on the operator's machine.
type MissingValueError struct {
	// Repo is the repo whose toolchain the variable belongs to, empty for a
	// box-scoped one.
	Repo string
	// Name is the variable as the staged document declares it.
	Name string
	// Path is the staged env its value was looked for in.
	Path string
}

// Error implements error.
func (e *MissingValueError) Error() string {
	return fmt.Sprintf("the variable %s%s has no staged value in %s: %s to stage one",
		e.scope(), e.Name, e.Path, restage)
}

// scope names the repo a variable is scoped to, so the refusal reads as the
// operator declared it: nothing at all for a box-scoped variable.
func (e *MissingValueError) scope() string {
	if e.Repo == "" {
		return ""
	}
	return "in repo " + e.Repo + " "
}

// EnvPathIn is where the resolved env sits under a box state directory. On-box
// smith reads EnvPathIn(Root); a test reads a directory of its own, and both
// agree with what the writer staged because the name comes from one place.
func EnvPathIn(root string) string {
	return filepath.Join(root, envFile)
}

// ReadValue answers with the staged value of one variable the staged blueprint
// declares, under root — /etc/smith on a real box. A variable is keyed by its
// scope and name: repo is the repo whose toolchain it belongs to, or empty for
// a box-scoped one.
//
// The value was resolved on the operator's machine and staged here, so nothing
// is resolved on the box: an `env:` reference names a variable in the
// operator's shell and a `file:` reference a path on their disk, and a box
// consulting its own environment or filesystem for either would export
// something else entirely, or nothing. A declared variable with no staged
// value is refused as a *MissingValueError naming it rather than falling back
// to what the box happens to hold.
func ReadValue(root, repo, name string) (string, error) {
	path := EnvPathIn(root)
	missing := &MissingValueError{Repo: repo, Name: name, Path: path}
	data, err := os.ReadFile(path) // #nosec G304 -- the staged env sits at a path smith derives itself.
	if err != nil {
		if os.IsNotExist(err) {
			return "", missing
		}
		return "", fmt.Errorf("read the staged value of %s at %s: %w", name, path, err)
	}
	var env Env
	if err := json.Unmarshal(data, &env); err != nil {
		return "", fmt.Errorf("read the staged env at %s: %w", path, err)
	}
	value, ok := env.scope(repo)[name]
	if !ok {
		return "", missing
	}
	return value, nil
}

// scope is the variables of one scope: the box's own, or a repo's overrides.
func (e Env) scope(repo string) map[string]string {
	if repo == "" {
		return e.Box
	}
	return e.Repos[repo]
}

// bytes renders the env as it is staged: JSON, indented, with a trailing
// newline. Keys sort themselves, so an env nothing has changed renders to the
// same bytes and a re-run rewrites nothing.
func (e Env) bytes() ([]byte, error) {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the resolved env: %w", err)
	}
	return append(data, '\n'), nil
}
