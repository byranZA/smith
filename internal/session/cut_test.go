package session_test

import (
	"context"
	"io"
	"path/filepath"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/misetrust"
	"github.com/byranZA/smith/internal/session"
)

// boxMise is where the toolchain step installs mise on a box.
const boxMise = "/home/smith/.local/bin/mise"

// miseGit runs every command for real, a command handed to boxMise, trusting or not, as mise exec runs it with no config to apply, and records each argv it was handed.
type miseGit struct {
	calls [][]string
}

func (g *miseGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	g.calls = append(g.calls, append([]string{name}, args...))
	if slices.Contains(append([]string{name}, args...), boxMise) {
		cmd := args[slices.Index(args, "--")+1:]
		name, args = cmd[0], cmd[1:]
	}
	return connection.System().Run(ctx, name, args, stdin, stdout, stderr)
}

// underMise is the calls of g that were handed to mise rather than to git.
func (g *miseGit) underMise() [][]string {
	var mise [][]string
	for _, c := range g.calls {
		if c[0] != "git" {
			mise = append(mise, c)
		}
	}
	return mise
}

// miseEnv is a box whose workspace holds the bare clone of "smith", with git run through g.
func miseEnv(t *testing.T, g *miseGit) session.Env {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepoWithRemote(t, workspace, "smith", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       g,
		Tmux:      &tmuxServer{},
		Mise:      boxMise,
	}
}

func TestStartOfANewBranchRunsOnlyItsFetchUnderMiseInTheRepoDirectory(t *testing.T) {
	t.Parallel()
	g := &miseGit{}
	env := miseEnv(t, g)

	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	repo := filepath.Join(env.Workspace, "smith")
	want := [][]string{{"sh", "-c", misetrust.Script, repo, boxMise, "-C", repo, "exec", "--", "git", "-C", filepath.Join(repo, "repo.git"), "fetch"}}
	if got := g.underMise(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Start(new branch spec-42) ran %q under mise, want %q", got, want)
	}
}

func TestStartOfAnExistingBranchRunsNoGitUnderMise(t *testing.T) {
	t.Parallel()
	g := &miseGit{}
	env := miseEnv(t, g)
	git(t, filepath.Join(env.Workspace, "smith", "repo.git"), "branch", "spec-42", "main")

	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := g.underMise(); len(got) != 0 {
		t.Errorf("Start(existing branch spec-42) ran %q under mise, want nothing", got)
	}
}

func TestSourceOfANewBranchRunsOnlyItsFetchUnderMiseInTheRepoDirectory(t *testing.T) {
	t.Parallel()
	g := &miseGit{}
	env := miseEnv(t, g)

	if _, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("SourceOf() err = %v", err)
	}

	repo := filepath.Join(env.Workspace, "smith")
	want := [][]string{{"sh", "-c", misetrust.Script, repo, boxMise, "-C", repo, "exec", "--", "git", "-C", filepath.Join(repo, "repo.git"), "fetch"}}
	if got := g.underMise(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("SourceOf(new branch spec-42) ran %q under mise, want %q", got, want)
	}
}
