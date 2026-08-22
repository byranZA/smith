package session_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
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
	// A bare clone carries none of the source's config, and the worktrees cut
	// from it are where the tests commit — so the identity lives here, not in
	// whatever ambient git config the machine running the tests happens to have.
	git(t, bare, "config", "user.email", "smith@example.com")
	git(t, bare, "config", "user.name", "smith")
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

// TestStartFetchesBeforeCuttingABranch locks in the fetch: a branch cut from a
// stale base is a merge-time failure that looks like anything but a session
// bug, so the one moment smith is already touching the network is the moment
// it refreshes the repo.
func TestStartFetchesBeforeCuttingABranch(t *testing.T) {
	workspace := t.TempDir()
	remote := writeBareRepoWithRemote(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	tip := pushToOrigin(t, remote, "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := gitOut(t, bare, "rev-parse", "refs/remotes/origin/main"); got != tip {
		t.Errorf("origin/main is at %s, want the tip the fetch should have brought down, %s", got, tip)
	}
}

// TestStartDoesNotFetchWhenNoBranchIsCut locks in the other half: the resume
// and connect paths cut nothing, so they cost no network.
func TestStartDoesNotFetchWhenNoBranchIsCut(t *testing.T) {
	tests := []struct {
		name  string
		stall func(tmux *tmuxServer)
	}{
		{"resuming a stopped session", func(tmux *tmuxServer) { tmux.kill(session.TmuxSession("smith-spec-42")) }},
		{"connecting to a live session", func(*tmuxServer) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			remote := writeBareRepoWithRemote(t, workspace, "smith", "main")
			bare := filepath.Join(workspace, "smith", "repo.git")
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
			stale := gitOut(t, bare, "rev-parse", "refs/remotes/origin/main")
			pushToOrigin(t, remote, "main")
			tt.stall(tmux)

			if _, err := session.Start(context.Background(), env, req); err != nil {
				t.Fatalf("Start() err = %v", err)
			}

			if got := gitOut(t, bare, "rev-parse", "refs/remotes/origin/main"); got != stale {
				t.Errorf("origin/main is at %s, want it left where the cut found it, %s", got, stale)
			}
		})
	}
}

// TestStartCutsTheBranchFromTheRequestedBase locks in --base: the operator's
// base wins over the one the blueprint declares for the repo.
func TestStartCutsTheBranchFromTheRequestedBase(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	git(t, bare, "branch", "release-2", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Base: "main"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	req := session.StartRequest{Repo: "smith", Branch: "spec-42", Base: "release-2"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if got, want := gitOut(t, dir, "rev-parse", "HEAD"), gitOut(t, bare, "rev-parse", "release-2"); got != want {
		t.Errorf("worktree is at %s, want the tip of the requested base %s", got, want)
	}
}

// TestStartRefusesABaseAgainstAnExistingBranch locks in that --base applies
// only when cutting: an operator naming a base for a branch that already
// exists has a rebase or a reset in mind, and neither is in smith's git
// surface.
func TestStartRefusesABaseAgainstAnExistingBranch(t *testing.T) {
	tests := []struct {
		name     string
		worktree bool
	}{
		{"a branch with no worktree", false},
		{"a branch a session already holds", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeBareRepo(t, workspace, "smith", "main")
			bare := filepath.Join(workspace, "smith", "repo.git")
			git(t, bare, "branch", "release-2", "main")
			tmux := &tmuxServer{}
			env := session.Env{
				Workspace: workspace,
				Repos:     []session.Repo{{Name: "smith"}},
				Git:       connection.System(),
				Tmux:      tmux,
			}
			if tt.worktree {
				if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
					t.Fatalf("stand the session up: %v", err)
				}
			} else {
				git(t, bare, "branch", "spec-42", "main")
			}
			before := worktreeCount(t, bare)

			_, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Base: "release-2"})

			if err == nil {
				t.Fatal("Start() err = nil, want a refusal naming the branch that already exists")
			}
			for _, want := range []string{"spec-42", "release-2"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Start() err = %v, want it to name %q", err, want)
				}
			}
			if got := worktreeCount(t, bare); got != before {
				t.Errorf("the repo holds %d worktrees, want the %d it held before the refusal", got, before)
			}
		})
	}
}

// TestStartRefusesADerivedNameHeldByADifferentBranch locks in the collision
// gate the lossy sanitizer admits: two branches deriving one name, and smith
// naming both rather than standing a second session up under a taken name.
func TestStartRefusesADerivedNameHeldByADifferentBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "smith/spec-42"}); err != nil {
		t.Fatalf("stand the first session up: %v", err)
	}

	_, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "smith-spec-42"})

	if err == nil {
		t.Fatal("Start() err = nil, want a refusal naming both branches")
	}
	for _, want := range []string{"smith/spec-42", "smith-spec-42"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Start() err = %v, want it to name the branch %q", err, want)
		}
	}
	if got := worktreeCount(t, bare); got != 1 {
		t.Errorf("the repo holds %d worktrees, want only the one the first session cut", got)
	}
	if n := tmux.ran("new-session"); n != 1 {
		t.Errorf("tmux new-session ran %d times, want no session under a taken name", n)
	}
}

// TestStartRefusesABranchCheckedOutElsewhere locks in that smith phrases git's
// one-worktree-per-branch rule itself, naming the session holding the branch
// rather than passing git's message through.
func TestStartRefusesABranchCheckedOutElsewhere(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	loose := filepath.Join(t.TempDir(), "loose")
	git(t, bare, "branch", "spec-42", "main")
	git(t, bare, "worktree", "add", loose, "spec-42")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}

	_, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})

	if err == nil {
		t.Fatal("Start() err = nil, want a refusal naming where the branch is checked out")
	}
	for _, want := range []string{"spec-42", loose, "smith-spec-42"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Start() err = %v, want it to name %q", err, want)
		}
	}
	if got := worktreeCount(t, bare); got != 1 {
		t.Errorf("the repo holds %d worktrees, want only the one already checked out", got)
	}
	if n := tmux.ran("new-session"); n != 0 {
		t.Errorf("tmux new-session ran %d times, want none for a refused start", n)
	}
}

// TestStartMaterialisesAnExistingBranch locks in the path a reclaimed session
// comes back on: the branch outlived its worktree in the bare repo, so start
// checks it out again rather than trying to cut it a second time.
func TestStartMaterialisesAnExistingBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	bare := filepath.Join(workspace, "smith", "repo.git")
	git(t, bare, "branch", "spec-42", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
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
	if branch := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "spec-42" {
		t.Errorf("worktree is on branch %q, want the branch that already existed, %q", branch, "spec-42")
	}
}

// pushToOrigin puts a commit on the origin's default branch the way a teammate
// does, and returns the tip it left there — which a repo that fetched can
// reach and a repo that did not cannot.
func pushToOrigin(t *testing.T, remote, branch string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "clone", remote, dir)
	commit(t, dir, "teammate")
	git(t, dir, "push", "origin", branch)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// worktreeCount counts the checkouts a bare repo's registry holds, which is
// how a test asserts that a refusal put nothing on disk.
func worktreeCount(t *testing.T, bare string) int {
	t.Helper()
	var n int
	for _, line := range strings.Split(gitOut(t, bare, "worktree", "list", "--porcelain"), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			n++
		}
	}
	return n - 1
}

// placer materializes a repo's declared placements from a staged tree the test
// wrote, wiring the real converge the way the command surface does rather than
// standing a fake in for it.
type placer struct {
	root       string
	placements []blueprint.Placement
}

func (p *placer) Place(repo, worktree string) error {
	if _, err := staging.Place(p.root, repo, worktree, p.placements); err != nil {
		return fmt.Errorf("place into %s: %w", worktree, err)
	}
	return nil
}

// stagePlacement writes the bytes of a repo placement where `machine setup`
// stages them, and answers with the placer that reads them back.
func stagePlacement(t *testing.T, repo string, p blueprint.Placement, content string) *placer {
	t.Helper()
	root := t.TempDir()
	path := staging.RepoPlacementPathIn(root, repo, p.To)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("make %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("stage %s: %v", path, err)
	}
	return &placer{root: root, placements: []blueprint.Placement{p}}
}

// TestStartPlacesTheDeclaredFilesInANewWorktree locks in the create path: a
// worktree smith cuts carries the files the blueprint declares for its repo.
func TestStartPlacesTheDeclaredFilesInANewWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	placed := stagePlacement(t, "smith", blueprint.Placement{From: "env:TOKEN", To: ".env"}, "TOKEN=staged\n")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      &fakeTmux{},
		Placer:    placed,
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if got := readFile(t, filepath.Join(dir, ".env")); got != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want the staged bytes %q", got, "TOKEN=staged\n")
	}
}

// TestStartReconvergesPlacementsOnResume locks in why stop then start is how
// an operator picks up changed config: standing a stopped session back up
// replaces the placed file with the staged bytes.
func TestStartReconvergesPlacementsOnResume(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	placed := stagePlacement(t, "smith", blueprint.Placement{To: ".env", Mode: "converge"}, "TOKEN=staged\n")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      tmux,
		Placer:    placed,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=edited\n"), 0o600); err != nil {
		t.Fatalf("edit the placed file: %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))

	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := readFile(t, filepath.Join(dir, ".env")); got != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want it restored from the staged bytes", got)
	}
}

// TestStartLeavesAWriteOncePlacementAloneOnResume locks in that the worktree
// owns a write-once file once it exists: a resume does not take it back.
func TestStartLeavesAWriteOncePlacementAloneOnResume(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	placed := stagePlacement(t, "smith", blueprint.Placement{To: ".env", Mode: "once"}, "TOKEN=staged\n")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      tmux,
		Placer:    placed,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=mine\n"), 0o600); err != nil {
		t.Fatalf("edit the placed file: %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))

	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := readFile(t, filepath.Join(dir, ".env")); got != "TOKEN=mine\n" {
		t.Errorf("placed file = %q, want the worktree's own copy left alone", got)
	}
}

// TestStartOnALiveSessionPlacesNothing locks in the asymmetry the slice is
// about: converging a file out from under a running process is never wanted,
// so the connect path leaves the worktree exactly as it is.
func TestStartOnALiveSessionPlacesNothing(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	placed := stagePlacement(t, "smith", blueprint.Placement{To: ".env", Mode: "converge"}, "TOKEN=staged\n")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith", Placements: []string{".env"}}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
		Placer:    placed,
	}
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("TOKEN=edited\n"), 0o600); err != nil {
		t.Fatalf("edit the placed file: %v", err)
	}

	if _, err := session.Start(context.Background(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := readFile(t, filepath.Join(dir, ".env")); got != "TOKEN=edited\n" {
		t.Errorf("placed file = %q, want it left as the live session had it", got)
	}
}

// readFile answers with the content at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
