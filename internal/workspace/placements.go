package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/byranZA/smith/internal/staging"
)

// onceMode is the placement mode that writes only when the destination is
// absent, after which the box owns the file.
const onceMode = "once"

// defaultPerms is the mode a placement carries when it declares none. A
// provisioned secret is readable by the account that consumes it and nothing
// else.
const defaultPerms = "0600"

// dirMode is the permission a directory made above a destination carries: the
// smith user's own, and no wider.
const dirMode = 0o700

// materialize puts one box-scoped placement on disk from the bytes `machine
// setup` staged under root, and reports what it did to the destination.
//
// The two modes are the whole of the rule. A converge placement is whole-file
// replacement, never a merge — smith does not parse the file, so it cannot
// merge keys — and the blueprint's bytes win over an edit made on the box. A
// once placement is written only when the destination is absent, and the box
// owns it thereafter; identity is path existence, so deleting the destination
// is how an operator asks for the bytes back.
//
// Either way the pass is check-before-change: a destination already holding
// the staged bytes is not rewritten, so a converged re-run moves no
// modification time.
//
// No source reference is resolved here, for any scheme. A staged `from:` names
// a path on the operator's machine and means nothing on the box, so a
// placement with no staged bytes is refused by name rather than read from a
// reference that would resolve to the wrong thing. The file is smith-owned by
// construction: on-box smith runs as the smith user, so what it writes belongs
// to that account.
func materialize(root string, p Placement) (string, error) {
	current, exists, err := onBox(p.Path)
	if err != nil {
		return "", err
	}
	if exists && p.Mode == onceMode {
		return "kept " + p.Path + ", which this box owns", nil
	}
	staged, err := staging.ReadPlacement(root, "", p.Destination)
	if err != nil {
		return "", fmt.Errorf("place the file at %s: %w", p.Path, err)
	}
	if exists && bytes.Equal(current, staged) {
		return "unchanged " + p.Path, nil
	}
	if err := put(p, staged); err != nil {
		return "", err
	}
	if exists {
		return "rewrote " + p.Path, nil
	}
	return "wrote " + p.Path, nil
}

// onBox reads what the box already holds at path. An absent file is the
// ordinary first-run case rather than a failure, so it reports as not existing
// and no error.
func onBox(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the blueprint's own destination, resolved against the smith user's home.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read what the box holds at %s: %w", path, err)
	}
	return data, true, nil
}

// put writes the staged bytes at the destination with the mode the placement
// declares, through a temporary path beside it, so the destination is never
// seen holding partial content or a wider mode than was asked for.
func put(p Placement, data []byte) error {
	mode, err := fileMode(p.Perms)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), dirMode); err != nil {
		return fmt.Errorf("make the directory above %s: %w", p.Path, err)
	}
	tmp := p.Path + ".placing"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("set the mode of %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p.Path); err != nil {
		return fmt.Errorf("move %s into place at %s: %w", tmp, p.Path, err)
	}
	return nil
}

// fileMode is the permission a placed file carries: the one the operator
// declared, or the owner-only default when they declared none. The blueprint
// has already refused anything that is not octal, so a value that fails to
// parse here is a staged document edited by hand.
func fileMode(declared string) (os.FileMode, error) {
	if declared == "" {
		declared = defaultPerms
	}
	parsed, err := strconv.ParseUint(declared, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("read the permissions %q of a placement: %w", declared, err)
	}
	return os.FileMode(parsed), nil
}
