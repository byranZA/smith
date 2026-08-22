package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
)

// fakeTmux stands in for the tmux binary, which needs a server and a TTY that
// no test may depend on. It records the argv it was handed and reports
// success.
type fakeTmux struct {
	calls [][]string
}

func (f *fakeTmux) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// TestSessionStartStandsUpAWorktreeAndATmuxSession drives the assembled
// command the way an operator on the box does, against a real bare repo, and
// checks what it did to the box rather than how it said it.
func TestSessionStartStandsUpAWorktreeAndATmuxSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	tmux := &fakeTmux{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("no worktree at %s: %v", dir, err)
	}
	if len(tmux.calls) != 1 {
		t.Fatalf("tmux invoked %d times, want 1: %v", len(tmux.calls), tmux.calls)
	}
	if got := out.String(); !strings.Contains(got, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session", got)
	}
}

// TestSessionStartRefusesAnUndeclaredRepo locks in the refusal exit path: the
// command reports on stderr and exits non-zero, and nothing is stood up.
func TestSessionStartRefusesAnUndeclaredRepo(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	tmux := &fakeTmux{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "ghost", "--branch", "spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an undeclared repo")
	}
	if !strings.Contains(errOut.String(), "ghost") {
		t.Errorf("stderr = %q, want it to name the undeclared repo", errOut.String())
	}
	if len(tmux.calls) != 0 {
		t.Errorf("tmux was invoked %v, want nothing stood up", tmux.calls)
	}
}

// TestSessionStartCutsFromTheRequestedBase locks in that --base reaches the
// cut: the branch starts at the ref the operator named, not at the repo's
// default branch.
func TestSessionStartCutsFromTheRequestedBase(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	bare := filepath.Join(workspace, "smith", "repo.git")
	runGit(t, bare, "branch", "release-2", "main")
	moveOn(t, bare, "main")
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--base", "release-2", "--detach"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if got, want := gitStdout(t, dir, "rev-parse", "HEAD"), gitStdout(t, bare, "rev-parse", "release-2"); got != want {
		t.Errorf("worktree is at %s, want the tip of the requested base %s", got, want)
	}
}

// TestSessionStartRefusesABaseAgainstAnExistingBranch locks in the refusal
// exit path for a base smith cannot honour: nothing is stood up and the
// command exits non-zero.
func TestSessionStartRefusesABaseAgainstAnExistingBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	bare := filepath.Join(workspace, "smith", "repo.git")
	runGit(t, bare, "branch", "release-2", "main")
	runGit(t, bare, "branch", "spec-42", "main")
	tmux := &fakeTmux{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--base", "release-2"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a base against an existing branch")
	}
	if !strings.Contains(errOut.String(), "spec-42") {
		t.Errorf("stderr = %q, want it to name the branch that already exists", errOut.String())
	}
	if len(tmux.calls) != 0 {
		t.Errorf("tmux was invoked %v, want nothing stood up", tmux.calls)
	}
}

// TestSessionStartIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionStartIsUnderTheRootCommand(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"session", "start"})
	if err != nil {
		t.Fatalf("Find(session start) err = %v", err)
	}
	if found.Name() != "start" {
		t.Errorf("Find(session start) = %q, want the start command", found.Name())
	}
}

// resolvedBox stands in for the configuration this box was built from, with
// the workspace pointed at a temp directory and one repo declared.
func resolvedBox(workspace string, repos ...string) boxResolver {
	declared := make([]blueprint.Repo, len(repos))
	for i, repo := range repos {
		declared[i] = blueprint.Repo{Name: repo, URL: "git@example.com:acme/" + repo + ".git"}
	}
	return func() (config.Resolved, error) {
		return config.Resolved{
			Workspace: config.Value{Value: workspace, Origin: config.FromBlueprint},
			Repos:     declared,
		}, nil
	}
}

// writeBareRepo stands up a bare repo in the workspace the way the workspace
// stage does — <workspace>/<repo>/repo.git — with one commit on main.
func writeBareRepo(t *testing.T, workspace, repo string) {
	t.Helper()
	src := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "smith@example.com"},
		{"config", "user.name", "smith"},
	} {
		runGit(t, src, args...)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, src, "add", "README.md")
	runGit(t, src, "commit", "-m", "base")
	if err := os.MkdirAll(filepath.Join(workspace, repo), 0o755); err != nil {
		t.Fatalf("make repo directory: %v", err)
	}
	runGit(t, src, "clone", "--bare", src, filepath.Join(workspace, repo, "repo.git"))
}

// moveOn puts a commit on a branch of a bare repo, so a test can tell a base
// that was honoured from the default branch that moved past it.
func moveOn(t *testing.T, bare, branch string) {
	t.Helper()
	tree := gitStdout(t, bare, "rev-parse", branch+"^{tree}")
	commit := gitStdout(t, bare, "-c", "user.email=smith@example.com", "-c", "user.name=smith",
		"commit-tree", tree, "-p", branch, "-m", "moved on")
	runGit(t, bare, "update-ref", "refs/heads/"+branch, commit)
}

// runGit runs a real git command in dir and fails the test if it does not.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitStdout(t, dir, args...)
}

// gitStdout runs a real git command in dir and returns its trimmed stdout.
func gitStdout(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var out, errOut strings.Builder
	err := connection.System().Run(context.Background(), "git", append([]string{"-C", dir}, args...), nil, &out, &errOut)
	if err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

var _ session.Runner = (*fakeTmux)(nil)

// fakeExec stands in for the exec boundary: a real exec would replace the test
// process, so the argv smith would have handed the kernel is recorded instead.
type fakeExec struct {
	calls [][]string
}

func (f *fakeExec) Exec(name string, args []string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

var _ session.Execer = (*fakeExec)(nil)

// TestSessionAttachObservesByDefault drives the assembled command the way an
// operator on the box does: attach with no flag hands the terminal to tmux
// read-only.
func TestSessionAttachObservesByDefault(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve)
	execer := &fakeExec{}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, execer))
	cmd.SetArgs([]string{"attach", "smith-spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	want := []string{"tmux", "attach-session", "-r", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || strings.Join(execer.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestSessionAttachInteractsOnRequest locks in the flag that asks for the
// writable access level on the same tmux session.
func TestSessionAttachInteractsOnRequest(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve)
	execer := &fakeExec{}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, execer))
	cmd.SetArgs([]string{"attach", "smith-spec-42", "--interact"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || strings.Join(execer.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestSessionAttachRefusesAnUnknownName locks in the refusal exit path: the
// command reports on stderr, exits non-zero, and connects to nothing.
func TestSessionAttachRefusesAnUnknownName(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	execer := &fakeExec{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), &fakeTmux{}, execer))
	cmd.SetArgs([]string{"attach", "smith-ghost"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an unknown session name")
	}
	if !strings.Contains(errOut.String(), "smith-ghost") {
		t.Errorf("stderr = %q, want it to name what was asked for", errOut.String())
	}
	if len(execer.calls) != 0 {
		t.Errorf("exec argv = %v, want nothing connected to", execer.calls)
	}
}

// TestSessionStartConnectsWritableUnlessDetached locks in the connect half of
// start: standing a session up and being put in it is one command, and
// --detach is the flag that opts out of it.
func TestSessionStartConnectsWritableUnlessDetached(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	execer := &fakeExec{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), &fakeTmux{}, execer))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	want := []string{"tmux", "attach-session", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || strings.Join(execer.calls[0], " ") != strings.Join(want, " ") {
		t.Fatalf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestSessionAttachIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionAttachIsUnderTheRootCommand(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"session", "attach"})
	if err != nil {
		t.Fatalf("Find(session attach) err = %v", err)
	}
	if found.Name() != "attach" {
		t.Errorf("Find(session attach) = %q, want the attach command", found.Name())
	}
}

// standUp stands a session up for branch spec-42 of repo smith, detached, so a
// test of another verb has one to address.
func standUp(t *testing.T, resolve boxResolver) {
	t.Helper()
	start := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}
}

// TestSessionListReportsTheSessionsOnTheBox drives the assembled command the
// way an operator on the box does: a session stood up a moment ago is listed
// under the name the other verbs take, with the table's four columns and the
// summary line.
func TestSessionListReportsTheSessionsOnTheBox(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	start := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "smith/spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}

	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"list"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	got := out.String()
	for _, want := range []string{"NAME", "STATE", "DIRTY", "UNPUSHED", "smith-smith-spec-42", "1 session"} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "smith/spec-42") {
		t.Errorf("stdout = %q, want the true branch left out of the default output", got)
	}
}

// TestSessionListSaysSoWhenThereIsNothingToList locks in that an empty box
// still prints the summary line and still exits zero.
func TestSessionListSaysSoWhenThereIsNothingToList(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"list"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code != 0 {
		t.Fatalf("exit code = %d, want 0 whatever list finds (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no sessions") {
		t.Errorf("stdout = %q, want it to report that there are no sessions", out.String())
	}
}

// TestSessionListIsUnderTheRootCommand locks in the surface an operator on the
// box types.
func TestSessionListIsUnderTheRootCommand(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"session", "list"})
	if err != nil {
		t.Fatalf("Find(session list) err = %v", err)
	}
	if found.Name() != "list" {
		t.Errorf("Find(session list) = %q, want the list command", found.Name())
	}
}

// TestSessionListExitsZeroWithUnpushedWork locks in that list reports what it
// found rather than failing on it: a non-zero exit on "something is unpushed"
// would conflate the command failing with the data having a property.
func TestSessionListExitsZeroWithUnpushedWork(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	start := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"list"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code != 0 {
		t.Fatalf("exit code = %d, want 0 with unpushed work present (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 with unpushed commits") {
		t.Errorf("stdout = %q, want the summary to report the unpushed work", out.String())
	}
}

// TestSessionListSubtractsTheBlueprintsPlacements locks in that the placement
// paths reach the dirty check from the blueprint the box was built from: a
// file smith placed itself is not the operator's work.
func TestSessionListSubtractsTheBlueprintsPlacements(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBoxPlacing(workspace, "smith", ".env")
	root := stageRepoPlacement(t, "smith", ".env", "TOKEN=1\n")
	start := newSessionCmd(onBox(t, resolve, root, connection.System(), &fakeTmux{}, &fakeExec{}))
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}
	cmd := newSessionCmd(onBox(t, resolve, root, connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"list"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	if strings.Contains(out.String(), "dirty") {
		t.Errorf("stdout = %q, want the placed file subtracted from the dirty check", out.String())
	}
}

// resolvedBoxPlacing stands in for a box whose blueprint declares placements
// into the repo's worktrees.
func resolvedBoxPlacing(workspace, repo string, to ...string) boxResolver {
	placements := make([]blueprint.Placement, len(to))
	for i, at := range to {
		placements[i] = blueprint.Placement{From: "file:secrets/env", To: at}
	}
	return func() (config.Resolved, error) {
		return config.Resolved{
			Workspace: config.Value{Value: workspace, Origin: config.FromBlueprint},
			Repos: []blueprint.Repo{{
				Name:       repo,
				URL:        "git@example.com:acme/" + repo + ".git",
				Placements: placements,
			}},
		}, nil
	}
}

// TestSessionStopEndsTheTmuxSessionAndKeepsTheWorktree drives the assembled
// command: the tmux session is killed by name and the checkout it was working
// in is still on the box afterwards.
func TestSessionStopEndsTheTmuxSessionAndKeepsTheWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	tmux := &fakeTmux{}
	cmd := newSessionCmd(onBox(t, resolvedBox(workspace, "smith"), t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("start Execute() err = %v (stderr: %s)", err, errOut.String())
	}
	out.Reset()
	cmd.SetArgs([]string{"stop", "smith-spec-42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("stop Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	var killed bool
	for _, argv := range tmux.calls {
		if strings.Contains(strings.Join(argv, " "), "kill-session -t smith/smith-spec-42") {
			killed = true
		}
	}
	if !killed {
		t.Errorf("tmux calls = %v, want the session killed by name", tmux.calls)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("worktree at %s did not survive the stop: %v", dir, err)
	}
	if got := out.String(); !strings.Contains(got, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session it stopped", got)
	}
}

// TestSessionStopIsUnderTheRootCommand locks in the surface an operator on the
// box types.
func TestSessionStopIsUnderTheRootCommand(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"session", "stop"})
	if err != nil {
		t.Fatalf("Find(session stop) err = %v", err)
	}
	if found.Name() != "stop" {
		t.Errorf("Find(session stop) = %q, want the stop command", found.Name())
	}
}

// stageRepoPlacement writes the bytes of a repo placement where `machine
// setup` stages them, and answers with the box state directory holding them.
func stageRepoPlacement(t *testing.T, repo, to, content string) string {
	t.Helper()
	root := t.TempDir()
	path := staging.RepoPlacementPathIn(root, repo, to)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("make %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("stage %s: %v", path, err)
	}
	return root
}

// TestSessionStartPlacesTheBlueprintsFilesInTheWorktree drives the assembled
// command end to end: the worktree it stands up carries the file the box's
// blueprint declares, from the bytes `machine setup` staged for it.
func TestSessionStartPlacesTheBlueprintsFilesInTheWorktree(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	root := stageRepoPlacement(t, "smith", "config/.env", "TOKEN=staged\n")
	cmd := newSessionCmd(onBox(t, resolvedBoxPlacing(workspace, "smith", "config/.env"), root, connection.System(), &fakeTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	placed := filepath.Join(workspace, "smith", "worktrees", "spec-42", "config", ".env")
	data, err := os.ReadFile(placed)
	if err != nil {
		t.Fatalf("read the placed file: %v", err)
	}
	if string(data) != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want the staged bytes", string(data))
	}
}

// stoppedTmux stands in for the tmux binary on a box where the session is not
// running: has-session fails the way tmux does when it holds no session of
// that name, which is how a test reaches the stopped half of the live probe.
type stoppedTmux struct {
	calls [][]string
}

func (s *stoppedTmux) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	s.calls = append(s.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "has-session" {
		return errNoTmuxSession
	}
	return nil
}

var _ session.Runner = (*stoppedTmux)(nil)

// errNoTmuxSession is what tmux exiting non-zero on has-session looks like
// here.
var errNoTmuxSession = errors.New("no such session")

// refusingReader stands in for the terminal a prompt would read from, and
// fails the test the moment anything reads it: rm must never ask.
type refusingReader struct {
	t *testing.T
}

func (r *refusingReader) Read([]byte) (int, error) {
	r.t.Error("the command read stdin, want no confirmation prompt ever")
	return 0, io.EOF
}

// TestSessionRemoveReclaimsTheWorktreeOfACleanStoppedSession drives the
// assembled command the way an operator on the box does: the checkout is gone
// and the branch it was on is still in the repo.
func TestSessionRemoveReclaimsTheWorktreeOfACleanStoppedSession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve)
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &stoppedTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived the removal: %v", dir, err)
	}
	bare := filepath.Join(workspace, "smith", "repo.git")
	if got := gitStdout(t, bare, "branch", "--list", "spec-42", "--format=%(refname:short)"); got != "spec-42" {
		t.Errorf("branch listing = %q, want the branch to survive the removal", got)
	}
	if got := out.String(); !strings.Contains(got, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session it removed", got)
	}
}

// TestSessionRemoveRefusesADirtyWorktreeWithoutPrompting locks in the refusal
// exit path: the enumeration and the force command go to stderr, the exit code
// is non-zero, the worktree is untouched, and nothing was ever asked.
func TestSessionRemoveRefusesADirtyWorktreeWithoutPrompting(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve)
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("modify the worktree: %v", err)
	}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), &stoppedTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(&refusingReader{t: t})

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a dirty worktree")
	}
	for _, want := range []string{"1 modified", "smith session rm smith-spec-42 --force"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to carry %q", errOut.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("worktree at %s did not survive the refusal: %v", dir, err)
	}
}

// TestSessionRemoveForceOverridesALiveDirtySession locks in that one flag
// covers both gates end to end: tmux is killed by name and the checkout is
// reclaimed.
func TestSessionRemoveForceOverridesALiveDirtySession(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve)
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatalf("modify the worktree: %v", err)
	}
	tmux := &fakeTmux{}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-spec-42", "--force"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	var killed bool
	for _, argv := range tmux.calls {
		if strings.Contains(strings.Join(argv, " "), "kill-session -t smith/smith-spec-42") {
			killed = true
		}
	}
	if !killed {
		t.Errorf("tmux calls = %v, want the live session killed by name", tmux.calls)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree at %s survived the forced removal: %v", dir, err)
	}
}

// TestSessionRemoveIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionRemoveIsUnderTheRootCommand(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"session", "rm"})
	if err != nil {
		t.Fatalf("Find(session rm) err = %v", err)
	}
	if found.Name() != "rm" {
		t.Errorf("Find(session rm) = %q, want the rm command", found.Name())
	}
}

// tmuxBox stands in for a tmux server that remembers what it started, which is
// what a test needs to put live and stopped sessions on one box.
type tmuxBox struct {
	live  map[string]bool
	calls [][]string
}

func (b *tmuxBox) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	if b.live == nil {
		b.live = map[string]bool{}
	}
	b.calls = append(b.calls, append([]string{name}, args...))
	if len(args) == 0 {
		return nil
	}
	id := tmuxTarget(args)
	switch args[0] {
	case "new-session":
		b.live[id] = true
	case "has-session":
		if !b.live[id] {
			return errNoTmuxSession
		}
	case "kill-session":
		if !b.live[id] {
			return errNoTmuxSession
		}
		delete(b.live, id)
	}
	return nil
}

// tmuxTarget returns the -t/-s value in a tmux argv.
func tmuxTarget(args []string) string {
	for i, a := range args {
		if (a == "-t" || a == "-s") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

var _ session.Runner = (*tmuxBox)(nil)

// standUpOn stands a session up through the assembled command, against the
// tmux server the test goes on to read.
func standUpOn(t *testing.T, resolve boxResolver, tmux session.Runner, repo, branch string) {
	t.Helper()
	runSession(t, resolve, tmux, "start", "--repo", repo, "--branch", branch, "--detach")
}

// stopSession ends a session through the assembled command, which is how a
// test reaches the stopped half of the live probe.
func stopSession(t *testing.T, resolve boxResolver, tmux session.Runner, name string) {
	t.Helper()
	runSession(t, resolve, tmux, "stop", name)
}

// runSession drives one session subcommand and fails the test if it refuses.
func runSession(t *testing.T, resolve boxResolver, tmux session.Runner, args ...string) {
	t.Helper()
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs(args)
	var errOut bytes.Buffer
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("session %s err = %v (stderr: %s)", strings.Join(args, " "), err, errOut.String())
	}
}

// twoReposOneLive stands three sessions up on two repos and stops two of them,
// which is the fixture every filter case is read against.
func twoReposOneLive(t *testing.T, workspace string) (boxResolver, *tmuxBox) {
	t.Helper()
	writeBareRepo(t, workspace, "smith")
	writeBareRepo(t, workspace, "web")
	resolve := resolvedBox(workspace, "smith", "web")
	tmux := &tmuxBox{}
	standUpOn(t, resolve, tmux, "smith", "live-one")
	standUpOn(t, resolve, tmux, "smith", "spec-42")
	standUpOn(t, resolve, tmux, "web", "hotfix")
	stopSession(t, resolve, tmux, "smith-spec-42")
	stopSession(t, resolve, tmux, "web-hotfix")
	return resolve, tmux
}

// TestSessionListNarrowsTheRowsByFilter drives the assembled command the way
// an operator on a busy box does: scale is answered by narrowing the flat
// table rather than by grouping it.
func TestSessionListNarrowsTheRowsByFilter(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)

	tests := []struct {
		name   string
		args   []string
		want   []string
		absent []string
	}{
		{"by repo", []string{"list", "--repo", "smith"}, []string{"smith-live-one", "smith-spec-42"}, []string{"web-hotfix"}},
		{"live only", []string{"list", "--live"}, []string{"smith-live-one"}, []string{"smith-spec-42", "web-hotfix"}},
		{"stopped only", []string{"list", "--stopped"}, []string{"smith-spec-42", "web-hotfix"}, []string{"smith-live-one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
			cmd.SetArgs(tt.args)
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
			}

			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout = %q, want it to list %q", out.String(), want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("stdout = %q, want %q filtered out", out.String(), absent)
				}
			}
		})
	}
}

// TestSessionListRefusesTwoOppositeStateFilters locks in that a filter pair
// that can never match anything is told plainly, rather than answered with an
// empty table that reads like a box with no sessions.
func TestSessionListRefusesTwoOppositeStateFilters(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"list", "--live", "--stopped"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for two opposite state filters")
	}
	if !strings.Contains(errOut.String(), "--stopped") {
		t.Errorf("stderr = %q, want it to name the filters that contradict", errOut.String())
	}
}

// TestSessionListNamesPrintsBareNames locks in the one output that is not for
// a human to read: names alone, one per line, so the shell can hand them
// straight to the removal verb.
func TestSessionListNamesPrintsBareNames(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"list", "--names"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	if got, want := out.String(), "smith-live-one\nsmith-spec-42\nweb-hotfix\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestSessionRemoveTakesSeveralNames drives the batch the way an operator
// clearing a box does: every worktree named is gone in one command.
func TestSessionRemoveTakesSeveralNames(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	for _, branch := range []string{"one", "two", "three"} {
		standUpOn(t, resolve, tmux, "smith", branch)
		stopSession(t, resolve, tmux, "smith-"+branch)
	}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-one", "smith-two", "smith-three"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errOut.String())
	}

	for _, branch := range []string{"one", "two", "three"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("worktree at %s survived the batch: %v", dir, err)
		}
		if !strings.Contains(out.String(), "smith-"+branch) {
			t.Errorf("stdout = %q, want it to report the removal of smith-%s", out.String(), branch)
		}
	}
}

// TestSessionRemoveRefusesTheWholeBatchAndEnumeratesEveryOffender locks in the
// all-or-nothing refusal end to end: both offenders are named, the exit is
// non-zero, and not one worktree was reclaimed.
func TestSessionRemoveRefusesTheWholeBatchAndEnumeratesEveryOffender(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	for _, branch := range []string{"one", "two", "three", "four"} {
		standUpOn(t, resolve, tmux, "smith", branch)
		stopSession(t, resolve, tmux, "smith-"+branch)
	}
	for _, branch := range []string{"two", "four"} {
		if err := os.WriteFile(filepath.Join(workspace, "smith", "worktrees", branch, "scratch.txt"), []byte("notes\n"), 0o600); err != nil {
			t.Fatalf("dirty the worktree: %v", err)
		}
	}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-one", "smith-two", "smith-three", "smith-four"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a batch with two dirty worktrees")
	}
	for _, want := range []string{"smith-two", "smith-four"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to name the offender %q", errOut.String(), want)
		}
	}
	for _, branch := range []string{"one", "two", "three", "four"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("worktree at %s did not survive the refused batch: %v", dir, err)
		}
	}
}

// TestSessionRemoveRefusesAnUnknownNameBeforeRemovingAnything locks in that a
// typo in a batch costs nothing rather than removing everything up to it.
func TestSessionRemoveRefusesAnUnknownNameBeforeRemovingAnything(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	for _, branch := range []string{"one", "two"} {
		standUpOn(t, resolve, tmux, "smith", branch)
		stopSession(t, resolve, tmux, "smith-"+branch)
	}
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-one", "smith-ghost", "smith-two"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if code := codeFromError(err); code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a name no session holds")
	}
	if !strings.Contains(errOut.String(), "smith-ghost") {
		t.Errorf("stderr = %q, want it to name the session it could not find", errOut.String())
	}
	for _, branch := range []string{"one", "two"} {
		dir := filepath.Join(workspace, "smith", "worktrees", branch)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("worktree at %s did not survive the refused batch: %v", dir, err)
		}
	}
}

// TestSessionRemoveTakesNoFiltersOfItsOwn locks in that filters live on the
// listing alone: a filter here would make the target set of a delete implicit,
// and a delete has no undo.
func TestSessionRemoveTakesNoFiltersOfItsOwn(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	standUpOn(t, resolve, tmux, "smith", "spec-42")
	stopSession(t, resolve, tmux, "smith-spec-42")
	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "--stopped"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := cmd.Execute()

	if err == nil {
		t.Fatal("Execute() err = nil, want --stopped rejected as an unknown flag")
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("worktree at %s did not survive the rejected flag: %v", dir, err)
	}
}

// TestSessionListNamesComposeIntoABatchRemoval drives the one pipeline the
// names output exists for: the stopped sessions the listing named are gone and
// the live one is untouched.
func TestSessionListNamesComposeIntoABatchRemoval(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)
	list := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	list.SetArgs([]string{"list", "--stopped", "--names"})
	var listed, errOut bytes.Buffer
	list.SetOut(&listed)
	list.SetErr(&errOut)
	if err := list.Execute(); err != nil {
		t.Fatalf("session list err = %v (stderr: %s)", err, errOut.String())
	}

	cmd := newSessionCmd(onBox(t, resolve, t.TempDir(), connection.System(), tmux, &fakeExec{}))
	cmd.SetArgs(append([]string{"rm"}, strings.Fields(listed.String())...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("session rm err = %v (stderr: %s)", err, errOut.String())
	}

	for _, gone := range []string{filepath.Join("smith", "worktrees", "spec-42"), filepath.Join("web", "worktrees", "hotfix")} {
		if _, err := os.Stat(filepath.Join(workspace, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("worktree at %s survived the batch: %v", gone, err)
		}
	}
	live := filepath.Join(workspace, "smith", "worktrees", "live-one")
	if _, err := os.Stat(live); err != nil {
		t.Errorf("the live session's worktree at %s did not survive: %v", live, err)
	}
}
