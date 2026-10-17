package session_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

func TestStartWithACommandMakesItTheTmuxSessionsRootProcess(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &fakeTmux{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42", Command: []string{"/usr/local/bin/smith", "loop", "run", "42"}}

	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-spec-42", "-c", dir, "/usr/local/bin/smith", "loop", "run", "42"}}
	if !reflect.DeepEqual(tmux.calls, want) {
		t.Errorf("Start(%+v) ran tmux %q, want %q", req, tmux.calls, want)
	}
}

func TestStartWithoutACommandLaunchesTheShell(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &fakeTmux{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}

	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-spec-42", "-c", dir}}
	if !reflect.DeepEqual(tmux.calls, want) {
		t.Errorf("Start(%+v) ran tmux %q, want %q", req, tmux.calls, want)
	}
}

func TestCloneIsTheDeclaredReposBareClone(t *testing.T) {
	env := session.Env{Workspace: "/home/dev/workspace", Repos: []session.Repo{{Name: "smith"}}}

	got, err := env.Clone("smith")
	if err != nil {
		t.Fatalf("Clone(%q) err = %v", "smith", err)
	}

	if want := "/home/dev/workspace/smith/repo.git"; got != want {
		t.Errorf("Clone(%q) = %q, want %q", "smith", got, want)
	}
}

func TestCloneRefusesAnUndeclaredRepo(t *testing.T) {
	env := session.Env{Workspace: "/home/dev/workspace", Repos: []session.Repo{{Name: "smith"}}}

	_, err := env.Clone("nope")

	want := `repo "nope" is not declared in the blueprint this box was built from; it declares smith`
	if err == nil || err.Error() != want {
		t.Errorf("Clone(%q) err = %v, want %q", "nope", err, want)
	}
}
