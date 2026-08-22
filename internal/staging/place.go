package staging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/byranZA/smith/internal/blueprint"
)

// onceMode is the placement mode that writes only if the destination is
// absent, after which the worktree owns the file.
const onceMode = "once"

// dirMode is the permission a directory made above a placement's destination
// carries. A worktree is the smith user's own, so the directories smith adds
// to it are readable by that account and its group and nothing wider.
const dirMode = 0o750

// Place materializes a repo's declared placements into a worktree from the
// bytes `machine setup` staged under root — /etc/smith on a real box — and
// reports what it did to each destination.
//
// It is the local half of Converge and keeps its two rules. A placement whose
// mode converges is whole-file replaced, so the worktree carries the
// blueprint's bytes and not an edit made in it; one declared "once" is written
// only when the destination is absent, and the worktree owns the file
// thereafter — deleting it is how an operator asks for it back. Either way the
// pass is check-before-change: a destination already holding the staged bytes
// is not rewritten, so nothing under a running session's feet moves.
//
// No source reference is resolved, for any scheme. A staged `from:` points at
// the operator's machine and means nothing here, so a placement with no staged
// bytes is refused as a *MissingPlacementError rather than read from a
// reference that would resolve to the wrong thing.
//
// The bytes land on a temporary path beside the destination and are moved into
// place, so a destination never exists holding partial content or a wider mode
// than the placement declares.
func Place(root, repo, worktree string, placements []blueprint.Placement) (Result, error) {
	var result Result
	for _, p := range placements {
		destination := filepath.Join(worktree, p.To)
		change, err := place(root, repo, p, destination)
		if err != nil {
			return Result{}, err
		}
		result.Entries = append(result.Entries, Entry{Path: destination, Change: change})
	}
	return result, nil
}

// place materializes one placement at destination and reports what it did.
func place(root, repo string, p blueprint.Placement, destination string) (Change, error) {
	current, exists, err := held(destination)
	if err != nil {
		return Unchanged, err
	}
	if exists && p.Mode == onceMode {
		return Unchanged, nil
	}
	staged, err := ReadPlacement(root, repo, p.To)
	if err != nil {
		return Unchanged, fmt.Errorf("place the file for repo %q at %s: %w", repo, destination, err)
	}
	if exists && bytes.Equal(current, staged) {
		return Unchanged, nil
	}
	if err := put(destination, staged, p.Perms); err != nil {
		return Unchanged, err
	}
	if exists {
		return Updated, nil
	}
	return Staged, nil
}

// held reads what the worktree already holds at destination. An absent file is
// the ordinary create-path case rather than a failure, so it reports as not
// existing and no error.
func held(destination string) ([]byte, bool, error) {
	data, err := os.ReadFile(destination) // #nosec G304 -- the destination is the blueprint's, under a worktree smith stood up.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read what the worktree holds at %s: %w", destination, err)
	}
	return data, true, nil
}

// put writes the bytes at destination with the mode the placement declares,
// through a temporary path beside it so the destination is never seen holding
// partial content.
func put(destination string, data []byte, declared string) error {
	mode, err := fileMode(declared)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), dirMode); err != nil {
		return fmt.Errorf("make the directory above %s: %w", destination, err)
	}
	tmp := destination + ".placing"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("set the mode of %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, destination); err != nil {
		return fmt.Errorf("move %s into place at %s: %w", tmp, destination, err)
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
