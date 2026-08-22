package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// TestListReportsUncommittedWorkAsDirty drives the dirty half of the
// work-state predicate against a real worktree: work git would report is
// dirty, and a file the repo ignores is not work.
func TestListReportsUncommittedWorkAsDirty(t *testing.T) {
	tests := []struct {
		name string
		work func(t *testing.T, dir string)
		want bool
	}{
		{"nothing touched", func(*testing.T, string) {}, false},
		{"a tracked file is modified", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "README.md"), "changed\n")
		}, true},
		{"a change is staged", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "README.md"), "staged\n")
			git(t, dir, "add", "README.md")
		}, true},
		{"an untracked file is present", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "scratch.txt"), "notes\n")
		}, true},
		{"an untracked file sits in an untracked directory", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
				t.Fatalf("make directory: %v", err)
			}
			writeFile(t, filepath.Join(dir, "tmp", "scratch.txt"), "notes\n")
		}, true},
		{"the file is ignored by the repo", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
			git(t, dir, "add", ".gitignore")
			git(t, dir, "commit", "-m", "ignore")
			writeFile(t, filepath.Join(dir, "ignored.txt"), "noise\n")
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeBareRepo(t, workspace, "smith", "main")
			env := session.Env{
				Workspace: workspace,
				Repos:     []session.Repo{{Name: "smith"}},
				Git:       connection.System(),
				Tmux:      &tmuxServer{},
			}
			if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
				t.Fatalf("Start() err = %v", err)
			}
			tt.work(t, filepath.Join(workspace, "smith", "worktrees", "spec-42"))

			got, err := session.List(context.Background(), env, session.Filter{})
			if err != nil {
				t.Fatalf("List() err = %v", err)
			}

			if len(got) != 1 {
				t.Fatalf("List() = %+v, want one session", got)
			}
			if got[0].Dirty != tt.want {
				t.Errorf("List() dirty = %v, want %v", got[0].Dirty, tt.want)
			}
		})
	}
}

// TestListSubtractsPlacedFilesFromTheDirtyCheck locks in that a file smith
// placed itself is not the operator's work, and that the subtraction reads the
// blueprint's placement paths rather than any ignore file: the exclude block
// belongs to the human's git status and is emptied here to prove it.
func TestListSubtractsPlacedFilesFromTheDirtyCheck(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env", "config/local.yaml"}}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, ".env"), "TOKEN=1\n")
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatalf("make config directory: %v", err)
	}
	writeFile(t, filepath.Join(dir, "config", "local.yaml"), "key: value\n")
	writeFile(t, filepath.Join(workspace, "smith", "repo.git", "info", "exclude"), "")

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want one session", got)
	}
	if got[0].Dirty {
		t.Errorf("List() dirty = true, want the placed files subtracted with the exclude file emptied")
	}
}

// TestListCountsAFileSmithDidNotPlaceAsDirty locks in that the subtraction is
// exactly the placement list: an untracked file no placement declares is the
// operator's work and is reported.
func TestListCountsAFileSmithDidNotPlaceAsDirty(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	writeFile(t, filepath.Join(dir, ".env"), "TOKEN=1\n")
	writeFile(t, filepath.Join(dir, "scratch.txt"), "notes\n")

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want one session", got)
	}
	if !got[0].Dirty {
		t.Errorf("List() dirty = false, want a file no placement declares to count as work")
	}
}

// writeFile writes a file the test needs in place and fails the test if it
// cannot.
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestListCountsCommitsNoRemoteHas locks in the unpushed half of the
// predicate: a branch smith cut has no upstream configured, and the count is
// still reported rather than erroring, because what the column claims is
// "safe on a remote" and nothing else.
func TestListCountsCommitsNoRemoteHas(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepoWithRemote(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	for _, message := range []string{"one", "two", "three"} {
		commit(t, dir, message)
	}
	if upstream := gitFails(t, dir, "rev-parse", "--abbrev-ref", "@{u}"); upstream == nil {
		t.Fatal("the branch has an upstream configured, want the no-upstream case under test")
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want one session", got)
	}
	if got[0].Unpushed != 3 {
		t.Errorf("List() unpushed = %d, want 3", got[0].Unpushed)
	}
}

// TestListCountsWorkThatReachedARemoteAsPushed locks in that the count asks
// whether any remote-tracking ref reaches the work, not whether an upstream
// was configured for it.
func TestListCountsWorkThatReachedARemoteAsPushed(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepoWithRemote(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	commit(t, dir, "one")
	git(t, dir, "push", "origin", "spec-42")

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want one session", got)
	}
	if got[0].Unpushed != 0 {
		t.Errorf("List() unpushed = %d, want 0 once the work is on a remote", got[0].Unpushed)
	}
}

// TestListReportsADetachedWorktree locks in that a worktree with no branch is
// still listed: there is nothing to count, which the readout says rather than
// the listing failing.
func TestListReportsADetachedWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepoWithRemote(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	dir := filepath.Join(workspace, "smith", "worktrees", "loose")
	git(t, bare, "worktree", "add", "--detach", dir, "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want the detached worktree listed", got)
	}
	if got[0].Branch != "" {
		t.Errorf("List() branch = %q, want a detached worktree to carry no branch", got[0].Branch)
	}
	if !strings.Contains(session.Readout(got), "-") {
		t.Errorf("Readout() = %q, want the unpushed count shown as not applicable", session.Readout(got))
	}
}

// writeBareRepoWithRemote stands up a bare repo in the workspace the way the
// workspace stage does, with a second bare repo as its origin and its
// remote-tracking refs fetched — which is what makes the unpushed count mean
// anything.
func writeBareRepoWithRemote(t *testing.T, workspace, repo, defaultBranch string) {
	t.Helper()
	writeBareRepo(t, workspace, repo, defaultBranch)
	bare := filepath.Join(workspace, repo, "repo.git")
	remote := filepath.Join(t.TempDir(), "origin.git")
	git(t, bare, "clone", "--bare", bare, remote)
	git(t, bare, "remote", "set-url", "origin", remote)
	// A bare clone configures no fetch refspec, so the remote-tracking refs
	// the count reads would never exist without one.
	git(t, bare, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	git(t, bare, "fetch", "origin")
}

// commit writes a file and commits it in a worktree, so a test can put commits
// on a branch the way an operator does.
func commit(t *testing.T, dir, message string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, message+".txt"), message+"\n")
	git(t, dir, "add", message+".txt")
	git(t, dir, "-c", "user.email=smith@example.com", "-c", "user.name=smith", "commit", "-m", message)
}

// gitFails runs a real git command and returns the error it failed with, so a
// test can assert on a command git is expected to refuse.
func gitFails(t *testing.T, dir string, args ...string) error {
	t.Helper()
	var out, errOut strings.Builder
	return connection.System().Run(context.Background(), "git", append([]string{"-C", dir}, args...), nil, &out, &errOut)
}
