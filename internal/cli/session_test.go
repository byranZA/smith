package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
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
	cmd := newSessionCmd(resolvedBox(workspace, "smith"), connection.System(), tmux)
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
	cmd := newSessionCmd(resolvedBox(workspace, "smith"), connection.System(), tmux)
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
func resolvedBox(workspace, repo string) boxResolver {
	return func() (config.Resolved, error) {
		return config.Resolved{
			Workspace: config.Value{Value: workspace, Origin: config.FromBlueprint},
			Repos:     []blueprint.Repo{{Name: repo, URL: "git@example.com:acme/" + repo + ".git"}},
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

// runGit runs a real git command in dir and fails the test if it does not.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	var out, errOut strings.Builder
	err := connection.System().Run(context.Background(), "git", append([]string{"-C", dir}, args...), nil, &out, &errOut)
	if err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
}

var _ session.Runner = (*fakeTmux)(nil)

// TestSessionListReportsTheSessionsOnTheBox drives the assembled command the
// way an operator on the box does: a session stood up a moment ago is listed
// under the name the other verbs take, with the table's four columns and the
// summary line.
func TestSessionListReportsTheSessionsOnTheBox(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	start := newSessionCmd(resolve, connection.System(), &fakeTmux{})
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "smith/spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}

	cmd := newSessionCmd(resolve, connection.System(), &fakeTmux{})
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
	cmd := newSessionCmd(resolvedBox(workspace, "smith"), connection.System(), &fakeTmux{})
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
	start := newSessionCmd(resolve, connection.System(), &fakeTmux{})
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}
	cmd := newSessionCmd(resolve, connection.System(), &fakeTmux{})
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
	start := newSessionCmd(resolve, connection.System(), &fakeTmux{})
	start.SetArgs([]string{"start", "--repo", "smith", "--branch", "spec-42", "--detach"})
	start.SetOut(io.Discard)
	start.SetErr(io.Discard)
	if err := start.Execute(); err != nil {
		t.Fatalf("session start err = %v", err)
	}
	placed := filepath.Join(workspace, "smith", "worktrees", "spec-42", ".env")
	if err := os.WriteFile(placed, []byte("TOKEN=1\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", placed, err)
	}
	cmd := newSessionCmd(resolve, connection.System(), &fakeTmux{})
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
