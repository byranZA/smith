package session_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// fakeTmux stands in for the tmux binary, which needs a server and a TTY that
// no test may depend on. It records the argv it was handed, which is what a
// test asserts on, and reports success.
type fakeTmux struct {
	calls [][]string
}

func (f *fakeTmux) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// TestStartCreatesTheWorktreeAndTheTmuxSession drives the create path against
// a real bare repo: the worktree lands one level below the repo's worktrees
// directory and a tmux session is launched in it.
func TestStartCreatesTheWorktreeAndTheTmuxSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &fakeTmux{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}

	got, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	want := session.Session{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Live: true}
	if got != want {
		t.Errorf("Start() = %+v, want %+v", got, want)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("worktree at %s does not hold the repo's files: %v", dir, err)
	}
	if branch := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "spec-42" {
		t.Errorf("worktree is on branch %q, want %q", branch, "spec-42")
	}
	if len(tmux.calls) != 1 {
		t.Fatalf("tmux invoked %d times, want 1: %v", len(tmux.calls), tmux.calls)
	}
	argv := strings.Join(tmux.calls[0], " ")
	for _, want := range []string{"tmux new-session", "-d", "-s smith/smith-spec-42", "-c " + dir} {
		if !strings.Contains(argv, want) {
			t.Errorf("tmux argv = %q, want it to contain %q", argv, want)
		}
	}
}

// writeBareRepo stands up a bare repo in the workspace the way the workspace
// stage does — <workspace>/<repo>/repo.git — with one commit on defaultBranch.
func writeBareRepo(t *testing.T, workspace, repo, defaultBranch string) {
	t.Helper()
	src := t.TempDir()
	git(t, src, "init", "-b", defaultBranch)
	git(t, src, "config", "user.email", "smith@example.com")
	git(t, src, "config", "user.name", "smith")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	git(t, src, "add", "README.md")
	git(t, src, "commit", "-m", "base")
	bare := filepath.Join(workspace, repo, "repo.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatalf("make repo directory: %v", err)
	}
	git(t, src, "clone", "--bare", src, bare)
}

// git runs a real git command in dir and fails the test if it does not.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOut(t, dir, args...)
}

// gitOut runs a real git command in dir and returns its trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var out, errOut strings.Builder
	err := connection.System().Run(context.Background(), "git", append([]string{"-C", dir}, args...), nil, &out, &errOut)
	if err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

// TestStartCutsTheBranchFromTheRepoDefaultBranch locks in where a new branch
// comes from: the repo's own default branch, not whatever a stale ref points
// at.
func TestStartCutsTheBranchFromTheRepoDefaultBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "trunk")
	bare := filepath.Join(workspace, "smith", "repo.git")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &fakeTmux{},
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if got, want := gitOut(t, dir, "rev-parse", "HEAD"), gitOut(t, bare, "rev-parse", "trunk"); got != want {
		t.Errorf("worktree is at %s, want the tip of the default branch %s", got, want)
	}
}

// TestStartCutsTheBranchFromTheDeclaredBase locks in that a repo declaring the
// branch its worktrees start from is cut from that one rather than from HEAD.
func TestStartCutsTheBranchFromTheDeclaredBase(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	git(t, bare, "branch", "release-2", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Base: "release-2"}},
		Git:       connection.System(),
		Tmux:      &fakeTmux{},
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if got := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "spec-42" {
		t.Fatalf("worktree is on branch %q, want %q", got, "spec-42")
	}
	if got, want := gitOut(t, dir, "rev-parse", "HEAD"), gitOut(t, bare, "rev-parse", "release-2"); got != want {
		t.Errorf("worktree is at %s, want the tip of the declared base %s", got, want)
	}
}

// TestStartSanitizesASlashedBranchToOneWorktreeDirectory locks in that a
// branch carrying a separator lands one level below the repo's worktrees
// directory rather than nesting below it.
func TestStartSanitizesASlashedBranchToOneWorktreeDirectory(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &fakeTmux{},
	}

	got, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "smith/spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got.Name != "smith-smith-spec-42" {
		t.Errorf("Start() name = %q, want %q", got.Name, "smith-smith-spec-42")
	}
	if got.Branch != "smith/spec-42" {
		t.Errorf("Start() branch = %q, want the branch git holds, %q", got.Branch, "smith/spec-42")
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "smith-spec-42")
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("worktree is not at %s: %v", dir, err)
	}
}

// TestStartRefusesAnUndeclaredRepo locks in that smith stands worktrees up
// only for the repos the blueprint declares, and names the ones it does.
func TestStartRefusesAnUndeclaredRepo(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &fakeTmux{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}

	_, err := session.Start(context.Background(), env, session.StartRequest{Repo: "ghost", Branch: "spec-42"})

	if err == nil {
		t.Fatal("Start() err = nil, want a refusal naming the undeclared repo")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "smith") {
		t.Errorf("Start() err = %v, want it to name both the undeclared repo and the declared ones", err)
	}
	if len(tmux.calls) != 0 {
		t.Errorf("tmux was invoked %v, want no session for an undeclared repo", tmux.calls)
	}
}

// TestStartRelaunchesTmuxInTheExistingWorktree drives the resume path: a
// worktree whose tmux session is gone gets a tmux session again, in the
// checkout the operator left, with no branch cut.
func TestStartRelaunchesTmuxInTheExistingWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, "left-behind.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("write into the worktree: %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))

	got, err := session.Start(context.Background(), env, req)
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	want := session.Session{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Live: true}
	if got != want {
		t.Errorf("Start() = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "left-behind.txt")); err != nil {
		t.Errorf("the existing worktree was not reused: %v", err)
	}
	if n := tmux.ran("new-session"); n != 2 {
		t.Errorf("tmux new-session ran %d times, want the resume to launch one", n)
	}
	sessions, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("List() = %+v, want the one session the resume reused", sessions)
	}
}

// TestStartOnALiveSessionCreatesNothing locks in the third path: start is
// ensure-running, so a session already running is reported and left alone.
func TestStartOnALiveSessionCreatesNothing(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}

	got, err := session.Start(context.Background(), env, req)
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	want := session.Session{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Live: true}
	if got != want {
		t.Errorf("Start() = %+v, want %+v", got, want)
	}
	if n := tmux.ran("new-session"); n != 1 {
		t.Errorf("tmux new-session ran %d times, want no second session for a live one", n)
	}
	sessions, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("List() = %+v, want no second worktree for a live session", sessions)
	}
}
