package staging

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// TestPlaceWritesTheStagedBytesIntoTheWorktree is the create path: a worktree
// that holds nothing at the destination receives the bytes staged for it.
func TestPlaceWritesTheStagedBytesIntoTheWorktree(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	stageRepoBytes(t, root, "smith", "config/.env", "TOKEN=staged\n")

	result, err := Place(root, "smith", worktree, []blueprint.Placement{{From: "env:TOKEN", To: "config/.env"}})
	if err != nil {
		t.Fatalf("Place() err = %v", err)
	}

	dest := filepath.Join(worktree, "config/.env")
	if got := readAt(t, dest); got != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want %q", got, "TOKEN=staged\n")
	}
	want := Result{Entries: []Entry{{Path: dest, Change: Staged}}}
	if len(result.Entries) != 1 || result.Entries[0] != want.Entries[0] {
		t.Errorf("Place() = %+v, want %+v", result, want)
	}
	if mode := statAt(t, dest).Mode().Perm(); mode != 0o600 {
		t.Errorf("placed file mode = %04o, want %04o", mode, 0o600)
	}
}

// TestPlaceConvergesAnEditedFile is the resume path: a placement whose mode
// converges replaces whatever the worktree holds with the staged bytes.
func TestPlaceConvergesAnEditedFile(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	stageRepoBytes(t, root, "smith", ".env", "TOKEN=staged\n")
	writeAt(t, filepath.Join(worktree, ".env"), "TOKEN=edited\n")

	result, err := Place(root, "smith", worktree, []blueprint.Placement{{To: ".env", Mode: "converge"}})
	if err != nil {
		t.Fatalf("Place() err = %v", err)
	}

	if got := readAt(t, filepath.Join(worktree, ".env")); got != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want the staged bytes back", got)
	}
	if got := result.Entries[0].Change; got != Updated {
		t.Errorf("change = %v, want %v", got, Updated)
	}
}

// TestPlaceLeavesAWriteOnceFileAlone locks in the once mode: the worktree owns
// the file once it exists, so a resume does not take the operator's edits back.
func TestPlaceLeavesAWriteOnceFileAlone(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	stageRepoBytes(t, root, "smith", ".env", "TOKEN=staged\n")
	writeAt(t, filepath.Join(worktree, ".env"), "TOKEN=mine\n")

	result, err := Place(root, "smith", worktree, []blueprint.Placement{{To: ".env", Mode: "once"}})
	if err != nil {
		t.Fatalf("Place() err = %v", err)
	}

	if got := readAt(t, filepath.Join(worktree, ".env")); got != "TOKEN=mine\n" {
		t.Errorf("placed file = %q, want it left as it was", got)
	}
	if got := result.Entries[0].Change; got != Unchanged {
		t.Errorf("change = %v, want %v", got, Unchanged)
	}
}

// TestPlaceWritesNothingWhenTheBytesAlreadyMatch keeps the pass
// check-before-change, so a converge that changes nothing does not move a
// file's modification time under a running process.
func TestPlaceWritesNothingWhenTheBytesAlreadyMatch(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	stageRepoBytes(t, root, "smith", ".env", "TOKEN=staged\n")
	dest := filepath.Join(worktree, ".env")
	writeAt(t, dest, "TOKEN=staged\n")
	before := statAt(t, dest).ModTime()

	result, err := Place(root, "smith", worktree, []blueprint.Placement{{To: ".env"}})
	if err != nil {
		t.Fatalf("Place() err = %v", err)
	}

	if got := result.Entries[0].Change; got != Unchanged {
		t.Errorf("change = %v, want %v", got, Unchanged)
	}
	if got := statAt(t, dest).ModTime(); !got.Equal(before) {
		t.Errorf("modification time moved to %v from %v, want it left alone", got, before)
	}
}

// TestPlaceCarriesTheDeclaredPermissions locks in that a placement declaring a
// mode lands with it, whatever the worktree already held.
func TestPlaceCarriesTheDeclaredPermissions(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()
	stageRepoBytes(t, root, "smith", "run.sh", "#!/bin/sh\n")
	dest := filepath.Join(worktree, "run.sh")
	writeAt(t, dest, "old\n")

	if _, err := Place(root, "smith", worktree, []blueprint.Placement{{To: "run.sh", Perms: "0755"}}); err != nil {
		t.Fatalf("Place() err = %v", err)
	}

	if mode := statAt(t, dest).Mode().Perm(); mode != 0o755 {
		t.Errorf("placed file mode = %04o, want %04o", mode, 0o755)
	}
}

// TestPlaceRefusesAPlacementWithNoStagedBytes reports the provisioning gap
// rather than falling back to a source reference that resolves only on the
// operator's machine.
func TestPlaceRefusesAPlacementWithNoStagedBytes(t *testing.T) {
	root := t.TempDir()
	worktree := t.TempDir()

	_, err := Place(root, "smith", worktree, []blueprint.Placement{{From: "file:/home/operator/.env", To: ".env"}})

	var missing *MissingPlacementError
	if !errors.As(err, &missing) {
		t.Fatalf("Place() err = %v, want a *MissingPlacementError", err)
	}
	if missing.Repo != "smith" || missing.Destination != ".env" {
		t.Errorf("refusal names %s/%s, want smith/.env", missing.Repo, missing.Destination)
	}
}

// stage writes the bytes of a repo-scoped placement where the writer would
// have staged them, so the reader under test finds them the same way.
func stageRepoBytes(t *testing.T, root, repo, destination, content string) {
	t.Helper()
	writeAt(t, RepoPlacementPathIn(root, repo, destination), content)
}

// write puts content at path, making the directories above it.
func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("make %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// read answers with the content at path.
func readAt(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// stat answers with the file information at path.
func statAt(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
