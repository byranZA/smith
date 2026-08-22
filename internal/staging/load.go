package staging

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/blueprint"
)

// documentFile is the staged document's name inside the box config directory.
// The writer and the reader both derive their path from it, so the two cannot
// disagree about where the document sits.
const documentFile = "blueprint.yaml"

// restage is what both refusals tell the operator to do. Neither refusal
// migrates anything and neither fetches anything: `machine setup` is the sole
// writer of the staged config, so it is the only fix for either.
const restage = "run `smith machine setup` from the operator's machine"

// AbsentError reports that no blueprint is staged on this box. It is a
// provisioning gap rather than a user error — the box was never given a
// document, or was given one before the config-staging stage existed — which
// is why it is a type of its own and worded apart from a malformed one.
type AbsentError struct {
	// Path is where the staged blueprint was looked for.
	Path string
}

// Error implements error.
func (e *AbsentError) Error() string {
	return fmt.Sprintf("no blueprint is staged on this box at %s: %s to stage one", e.Path, restage)
}

// MalformedError reports that the staged blueprint cannot be trusted: it is
// not YAML at all, or it does not fit the schema. On-box smith runs the same
// strict parser as the operator's, so a document that passed on the way out
// and fails here has been truncated or hand-edited on the box.
type MalformedError struct {
	// Path is the staged blueprint that could not be read.
	Path string
	// Err is the parse refusal, naming every problem and its line.
	Err error
}

// Error implements error.
func (e *MalformedError) Error() string {
	return fmt.Sprintf("the staged blueprint at %s is malformed: %v\n%s to re-stage it", e.Path, e.Err, restage)
}

// Unwrap gives up the parse refusal underneath.
func (e *MalformedError) Unwrap() error { return e.Err }

// DocumentPathIn is where the staged blueprint sits under a box config
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
func Load(root string) (blueprint.Blueprint, error) {
	path := DocumentPathIn(root)
	data, err := os.ReadFile(path) // #nosec G304 -- the staged document sits at a path smith derives itself.
	if err != nil {
		if os.IsNotExist(err) {
			return blueprint.Blueprint{}, &AbsentError{Path: path}
		}
		return blueprint.Blueprint{}, fmt.Errorf("read staged blueprint %s: %w", path, err)
	}
	b, err := blueprint.Parse(data)
	if err != nil {
		return blueprint.Blueprint{}, &MalformedError{Path: path, Err: err}
	}
	return b, nil
}
