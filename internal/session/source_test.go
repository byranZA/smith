package session_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// sourceEnv is a box whose repo "smith" has an origin, returned with the
// origin's path, so a test can move the base past the bare clone's copy.
func sourceEnv(t *testing.T) (session.Env, string) {
	t.Helper()
	workspace := t.TempDir()
	remote := writeBareRepoWithRemote(t, workspace, "smith", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}, remote
}

func TestSourceOfANewBranchIsTheFetchedBaseItWillBeCutFrom(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)
	tip := pushToOrigin(t, remote, "main")

	got, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	if want := (session.Source{Ref: tip, Cut: true}); got != want {
		t.Errorf("SourceOf() = %+v, want %+v", got, want)
	}
}

func TestSourceOfNoBranchYetIsTheFetchedBase(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)
	tip := pushToOrigin(t, remote, "main")

	got, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith"})
	if err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	if want := (session.Source{Ref: tip, Cut: true}); got != want {
		t.Errorf("SourceOf() = %+v, want %+v", got, want)
	}
}

func TestSourceOfANewBranchCreatesNothing(t *testing.T) {
	t.Parallel()
	env, _ := sourceEnv(t)
	bare := filepath.Join(env.Workspace, "smith", "repo.git")

	if _, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	if n := worktreeCount(t, bare); n != 0 {
		t.Errorf("bare repo holds %d worktrees, want none", n)
	}
	if listed := gitOut(t, bare, "branch", "--list", "spec-42"); listed != "" {
		t.Errorf("branch spec-42 exists after SourceOf: %q", listed)
	}
}

func TestSourceOfABranchWithNoCheckoutIsItsTip(t *testing.T) {
	t.Parallel()
	env, _ := sourceEnv(t)
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	git(t, bare, "branch", "spec-42", "main")
	tip := gitOut(t, bare, "rev-parse", "spec-42")

	got, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	if want := (session.Source{Ref: tip}); got != want {
		t.Errorf("SourceOf() = %+v, want %+v", got, want)
	}
}

func TestSourceOfABranchInACheckoutIsTheCheckout(t *testing.T) {
	t.Parallel()
	env, _ := sourceEnv(t)
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	got, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	dir := gitOut(t, filepath.Join(env.Workspace, "smith", "worktrees", "spec-42"), "rev-parse", "--show-toplevel")
	if want := (session.Source{Dir: dir}); got != want {
		t.Errorf("SourceOf() = %+v, want %+v", got, want)
	}
}

func TestSourceOfRefusesABaseForABranchThatExists(t *testing.T) {
	t.Parallel()
	env, _ := sourceEnv(t)
	git(t, filepath.Join(env.Workspace, "smith", "repo.git"), "branch", "spec-42", "main")

	_, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Base: "main"})

	if err == nil || !strings.Contains(err.Error(), "already exists, so start cannot base it on") {
		t.Errorf("SourceOf() err = %v, want the refusal Start gives", err)
	}
}
