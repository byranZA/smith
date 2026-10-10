package staging

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/hint"
)

// documentFile is the staged document's name inside the box state directory.
// The writer and the reader both derive their path from it, so the two cannot
// disagree about where the document sits.
const documentFile = "blueprint.yaml"

// restage is what every staged-config refusal tells the operator to do, spelled
// for the command that met it: relayed, it names the box the operator relayed
// to, and typed on the box it cannot, because the box does not know the name
// its operator gave it. None of the refusals migrates or fetches anything:
// `machine setup` is the sole writer of the staged config, so it is the only fix.
func restage(inv hint.Invocation) string {
	setup := "run `" + inv.Command("machine setup") + "`"
	if inv.Box != "" {
		return setup
	}
	return setup + " from the operator's machine"
}

// AbsentError reports that a file on-box smith needs — the staged blueprint or
// the staged resolution — is not staged on this box. It is a provisioning gap
// rather than a user error — the box was never given one, or was given one by
// a smith that predates it — which is why it is a type of its own and worded
// apart from a malformed one.
type AbsentError struct {
	// Path is where the staged file was looked for.
	Path string
	// Hint is the command that met the refusal, which the commands it
	// suggests are spelled for.
	Hint hint.Invocation
}

// Error implements error. Typed rather than relayed, the refusal may have been
// met on the operator's own machine by a command that left out its box, so it
// also names the command with one; nothing guesses which machine this is,
// because a machine may be its own box.
func (e *AbsentError) Error() string {
	refusal := fmt.Sprintf("no %s is staged on this box at %s: %s to stage one", artifact(e.Path), e.Path, restage(e.Hint))
	if e.Hint.Box != "" || e.Hint.Verb == "" {
		return refusal
	}
	return refusal + "\nif this is the operator's machine, name the box the command is for: `" + e.Hint.Rerun() + "`"
}

// MalformedError reports that a staged file cannot be trusted: a blueprint
// that is not YAML or does not fit the schema, or a resolution that is not
// JSON. On-box smith runs the same strict parser as the operator's, so a file
// that passed on the way out and fails here has been truncated or hand-edited
// on the box.
type MalformedError struct {
	// Path is the staged file that could not be read.
	Path string
	// Err is the parse refusal, naming every problem and its line.
	Err error
	// Hint is the command that met the refusal, which the command it
	// suggests is spelled for.
	Hint hint.Invocation
}

// Error implements error.
func (e *MalformedError) Error() string {
	return fmt.Sprintf("the staged %s at %s is malformed: %v\n%s to re-stage it", artifact(e.Path), e.Path, e.Err, restage(e.Hint))
}

// artifact names the staged file at path the way the operator knows it, so a
// refusal reads the same whichever of the two it is about.
func artifact(path string) string {
	if filepath.Base(path) == resolutionFile {
		return "resolution"
	}
	return "blueprint"
}

// Unwrap gives up the parse refusal underneath.
func (e *MalformedError) Unwrap() error { return e.Err }

// DocumentPathIn is where the staged blueprint sits under a box state
// directory. On-box smith reads DocumentPathIn(Root); a test reads a directory
// of its own, and both agree with what the writer staged because the name comes
// from one place.
func DocumentPathIn(root string) string {
	return filepath.Join(root, documentFile)
}

// Load reads the blueprint staged under root — /etc/smith on a real box — and
// validates it through the same strict pass as the operator-side load: an
// unknown key is an error with the line it appears on, exactly as it would have
// been on the operator's machine.
//
// This is not schema skew reintroduced. Version skew is refused before any verb
// runs, so the two passes are the same parser; what a second pass catches is a
// document edited or truncated after it was staged.
//
// The two refusals are types callers branch on and are deliberately worded
// apart: *AbsentError is a provisioning gap, *MalformedError a document that
// cannot be trusted. Both name `machine setup`, which is the only writer of
// either, and neither migrates anything.
//
// No source reference in the document is resolved here. Every `from:` in a
// staged blueprint points at the operator's laptop and is opaque provenance on
// the box; a placement's bytes come from the staged tree instead.
//
// inv is the command the blueprint is read for, which a refusal's suggested
// commands are spelled for.
func Load(root string, inv hint.Invocation) (blueprint.Blueprint, error) {
	path := DocumentPathIn(root)
	data, err := os.ReadFile(path) // #nosec G304 -- the staged document sits at a path smith derives itself.
	if err != nil {
		if os.IsNotExist(err) {
			return blueprint.Blueprint{}, &AbsentError{Path: path, Hint: inv}
		}
		return blueprint.Blueprint{}, fmt.Errorf("read staged blueprint %s: %w", path, err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		return blueprint.Blueprint{}, &MalformedError{Path: path, Err: err, Hint: inv}
	}
	return b, nil
}

// MissingPlacementError reports a placement the staged blueprint declares
// whose bytes are not staged beside it. `machine setup` writes the document and
// the bytes in one run, so the two only come apart when /etc/smith/ has been
// edited by hand — and the placement cannot be honoured from the box, because
// its source reference resolves only on the operator's machine.
type MissingPlacementError struct {
	// Repo is the repo whose worktree the file was to land in, empty for a
	// box-scoped placement.
	Repo string
	// Destination is where the file was to land, as the staged document
	// declared it.
	Destination string
	// Path is the staged path its bytes were looked for at.
	Path string
	// Hint is the command that met the refusal, which the command it
	// suggests is spelled for.
	Hint hint.Invocation
}

// Error implements error.
func (e *MissingPlacementError) Error() string {
	return fmt.Sprintf("the placement to %s%s has no staged bytes at %s: %s to stage them",
		e.scope(), e.Destination, e.Path, restage(e.Hint))
}

// scope names the repo a placement is scoped to, so the refusal reads as the
// operator declared it: nothing at all for a box-scoped placement.
func (e *MissingPlacementError) scope() string {
	if e.Repo == "" {
		return ""
	}
	return "in repo " + e.Repo + " "
}

// ReadPlacement answers with the staged bytes of a placement the staged
// blueprint declares, under root — /etc/smith on a real box. A placement is
// keyed by its scope and destination: repo is the repo whose worktree the file
// lands in, or empty for a box-scoped placement.
//
// The key comes from the same pure derivation the writer staged the bytes
// under, so the reader cannot look in a place the writer would not have
// written. A box destination is canonicalized the same way too, which is why
// ~/.npmrc reads back the bytes staged for /home/smith/.npmrc.
//
// The placement's `from:` is not consulted, for any scheme. Every source
// reference in a staged document points at the operator's laptop — a path that
// does not exist on the box, or an environment variable that means something
// else there — so it is opaque provenance and the staged bytes are the only
// truth. A declared placement with no staged bytes is refused as a
// *MissingPlacementError naming the destination rather than falling back to a
// reference that would resolve to the wrong thing, its suggested command
// spelled for inv.
func ReadPlacement(root, repo, destination string, inv hint.Invocation) ([]byte, error) {
	path := BoxPlacementPathIn(root, destination)
	if repo != "" {
		path = RepoPlacementPathIn(root, repo, destination)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the staged path is one smith derives itself from the scope and destination.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &MissingPlacementError{Repo: repo, Destination: destination, Path: path, Hint: inv}
		}
		return nil, fmt.Errorf("read the staged bytes of the placement to %s at %s: %w", destination, path, err)
	}
	return data, nil
}
