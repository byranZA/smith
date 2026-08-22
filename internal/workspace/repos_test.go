package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// forge is the remote a declared repo is cloned from. No test reaches it: the
// box the stage runs against is a fake, and the url is only ever compared and
// passed along.
const forge = "https://forge.test/acme.git"

// clonePath is where a repo's bare clone lands under a home, for the default
// workspace root.
func clonePath(home, name string) string {
	return filepath.Join(home, "workspace", name, "repo.git")
}

// declaresRepo is a blueprint declaring one repo cloned from the forge.
func declaresRepo(name string) blueprint.Blueprint {
	return blueprint.Blueprint{Repos: []blueprint.Repo{{Name: name, URL: forge}}}
}

// alreadyCloned puts a bare clone of url on the box the way an earlier run
// would have left one, so a test can ask what a second converge does to it.
func alreadyCloned(t *testing.T, box *fakeBox, path, url string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("put an existing clone at %s: %v", path, err)
	}
	box.remotes[path] = url
}

// TestConvergeClonesADeclaredRepoBare proves a declared repo becomes a bare
// clone at ~/workspace/<repo>/repo.git.
func TestConvergeClonesADeclaredRepoBare(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true

	result, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if want := "git clone --bare " + forge + " " + clonePath(home, "acme"); !box.ran(want) {
		t.Errorf("the stage ran %v, want it to run %q", box.calls, want)
	}
	if !strings.Contains(progress, clonePath(home, "acme")) {
		t.Errorf("progress = %q, want the clone reported as it completes", progress)
	}
}

// TestConvergeClonesUnderMiseExec proves the clone runs under mise, which is
// what puts the blueprint's box-level env — a forge token among it — in scope
// for a token-authenticated clone.
func TestConvergeClonesUnderMiseExec(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true

	convergeOnBox(t, box, home, declaresRepo("acme"))

	if !box.ran("mise exec -- git clone") {
		t.Errorf("the stage ran %v, want the clone run under mise exec", box.calls)
	}
}

// TestConvergeClonesAfterThePlacementsThatAuthenticateIt proves a private repo
// clones using a placed credential: the box's git identity is already on disk
// by the time the stage runs the first command of a repo's clone.
func TestConvergeClonesAfterThePlacementsThatAuthenticateIt(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.gitconfig", "[user]\n")
	path := filepath.Join(home, ".gitconfig")
	watcher := &watchingBox{fakeBox: *newBox(), path: path}
	watcher.hasMise = true
	b := declaresRepo("acme")
	b.Placements = []blueprint.Placement{{From: "file:/home/op/gitconfig", To: "~/.gitconfig", Mode: "converge"}}

	var progress bytes.Buffer
	env := Env{Command: watcher, StateRoot: root, Secret: fakeSecret}
	if _, err := Converge(context.Background(), env, Plan(b, home), &progress); err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}

	if !watcher.placed {
		t.Errorf("the stage ran a command before %s was on the box", path)
	}
}

// TestConvergeNamesARepoFromItsURL proves an unnamed repo lands in the
// directory its url's last path segment names, without the .git a remote
// carries.
func TestConvergeNamesARepoFromItsURL(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true

	result, _ := convergeOnBox(t, box, home, declaresRepo(""))

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if !box.ran(clonePath(home, "acme")) {
		t.Errorf("the stage ran %v, want the clone at %s", box.calls, clonePath(home, "acme"))
	}
}

// TestConvergeClonesUnderTheDeclaredWorkspaceRoot proves the blueprint's
// workspace field overrides the default root the clones land under.
func TestConvergeClonesUnderTheDeclaredWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	b := declaresRepo("acme")
	b.Workspace = "~/code"

	convergeOnBox(t, box, home, b)

	if want := filepath.Join(home, "code", "acme", "repo.git"); !box.ran(want) {
		t.Errorf("the stage ran %v, want the clone at %s", box.calls, want)
	}
}

// TestConvergeCreatesNoWorktree proves the stage stops at the bare clone. A
// converged box is one a session can be started on, not one that already
// holds a checkout.
func TestConvergeCreatesNoWorktree(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true

	convergeOnBox(t, box, home, declaresRepo("acme"))

	if _, err := os.Stat(filepath.Join(home, "workspace", "acme", "worktrees")); !os.IsNotExist(err) {
		t.Errorf("a worktrees directory exists after the stage ran (%v), want none", err)
	}
	if box.ran("worktree") {
		t.Errorf("the stage ran %v, want no worktree command", box.calls)
	}
}

// TestConvergeWritesNoRepoScopedPlacement proves a repo's own placements are
// left for the session that stands a worktree up: they are worktree-relative,
// and this stage makes no worktree to write them into.
func TestConvergeWritesNoRepoScopedPlacement(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	b := declaresRepo("acme")
	b.Repos[0].Placements = []blueprint.Placement{{From: "file:/home/op/env", To: "config/local.env", Mode: "converge"}}

	result, _ := convergeOnBox(t, box, home, b)

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if _, err := os.Stat(filepath.Join(home, "workspace", "acme", "config", "local.env")); !os.IsNotExist(err) {
		t.Errorf("a repo-scoped placement was written (%v), want none by this stage", err)
	}
}

// TestConvergeFetchesAnExistingClone proves an existing clone of the declared
// url is fetched rather than deleted and cloned again.
func TestConvergeFetchesAnExistingClone(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	path := clonePath(home, "acme")
	alreadyCloned(t, box, path, forge)

	result, _ := convergeOnBox(t, box, home, declaresRepo("acme"))

	if result.Failed() {
		t.Fatalf("Result.Failed() = true, want false: %s", result.Report())
	}
	if box.ran("clone") {
		t.Errorf("the stage ran %v, want the existing clone fetched rather than recloned", box.calls)
	}
	if !box.ran("fetch") {
		t.Errorf("the stage ran %v, want it to fetch the existing clone", box.calls)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the existing clone is gone after the stage ran: %v", err)
	}
}

// TestConvergeRefusesAClonesChangedURL proves a clone whose remote no longer
// matches the blueprint is reported and left alone: repointing a bare repo
// that already has worktrees would change what every existing branch tracks.
func TestConvergeRefusesAClonesChangedURL(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	path := clonePath(home, "acme")
	alreadyCloned(t, box, path, "https://forge.test/somewhere-else.git")

	result, progress := convergeOnBox(t, box, home, declaresRepo("acme"))

	if !result.Failed() {
		t.Fatalf("Result.Failed() = false, want the url mismatch reported: %s", result.Report())
	}
	if !strings.Contains(progress, "somewhere-else.git") || !strings.Contains(progress, forge) {
		t.Errorf("progress = %q, want both urls named in the refusal", progress)
	}
	if box.ran("clone") || box.ran("fetch") {
		t.Errorf("the stage ran %v, want the mismatched clone left untouched", box.calls)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the mismatched clone is gone after the stage ran: %v", err)
	}
}
