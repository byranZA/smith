package session_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// TestStopEndsTheTmuxSessionAndKeepsTheWorktree locks in what stop costs: the
// process ends, and the checkout and the branch it was working on survive it.
func TestStopEndsTheTmuxSessionAndKeepsTheWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if err := session.Stop(context.Background(), env, started.Name); err != nil {
		t.Fatalf("Stop() err = %v", err)
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List() = %+v, want the stopped session still listed", got)
	}
	if got[0].Live {
		t.Errorf("List() reported %q live, want stopped after Stop()", got[0].Name)
	}
	if got[0].Branch != "spec-42" {
		t.Errorf("List() branch = %q, want the branch to survive the stop", got[0].Branch)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("worktree at %s did not survive the stop: %v", dir, err)
	}
}

// TestStopIsANoOpOnAStoppedSession locks in the idempotence stop is owed: it
// has no safety gate because there is nothing to lose, and stopping twice must
// therefore succeed and leave the box exactly as it was.
func TestStopIsANoOpOnAStoppedSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	started, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	if err := session.Stop(context.Background(), env, started.Name); err != nil {
		t.Fatalf("first Stop() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	before := tree(t, dir)

	if err := session.Stop(context.Background(), env, started.Name); err != nil {
		t.Fatalf("second Stop() err = %v", err)
	}

	if after := tree(t, dir); after != before {
		t.Errorf("worktree = %q after a second Stop(), want it unchanged at %q", after, before)
	}
}

// tree renders the paths under dir, so a test can assert that a verb changed
// nothing on disk.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n")
}
