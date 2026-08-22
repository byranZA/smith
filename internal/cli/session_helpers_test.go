package cli

import (
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
// success, so every session it is asked about reads as live.
type fakeTmux struct {
	calls [][]string
}

func (f *fakeTmux) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

var _ session.Runner = (*fakeTmux)(nil)

// errNoTmuxSession is what tmux exiting non-zero on has-session looks like
// here.
var errNoTmuxSession = errors.New("no such session")

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

var _ session.Runner = (*tmuxBox)(nil)

// tmuxTarget returns the -t/-s value in a tmux argv.
func tmuxTarget(args []string) string {
	for i, a := range args {
		if (a == "-t" || a == "-s") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// fakeExec stands in for the exec boundary: a real exec would replace the test
// process, so the argv smith would have handed the kernel is recorded instead.
type fakeExec struct {
	calls [][]string
}

func (f *fakeExec) Exec(name string, args []string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// line returns the argv of the one connection the command made, joined for
// readability, and fails the test if it did not make exactly one.
func (f *fakeExec) line(t *testing.T) string {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("exec called %d times, want 1: %v", len(f.calls), f.calls)
	}
	return strings.Join(f.calls[0], " ")
}

var _ session.Execer = (*fakeExec)(nil)

// resolvedBox stands in for the configuration this box was built from, with
// the workspace pointed at a temp directory and the named repos declared.
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

// onBoxWiring assembles the session verbs as the operator who has connected to
// the box gets them, against the workspace and tmux server a test drives.
func onBoxWiring(t *testing.T, resolve boxResolver, tmux session.Runner, connect session.Execer) sessionWiring {
	t.Helper()
	return onBox(t, resolve, t.TempDir(), connection.System(), tmux, connect)
}

// dirtyWorktree writes an untracked file into a session's checkout, which is
// the state the removal gate refuses on.
func dirtyWorktree(t *testing.T, workspace, repo, branch string) {
	t.Helper()
	path := filepath.Join(workspace, repo, "worktrees", branch, "scratch.txt")
	if err := os.WriteFile(path, []byte("notes\n"), 0o600); err != nil {
		t.Fatalf("dirty the worktree: %v", err)
	}
}

// standUp stands a session up through the assembled command, detached, against
// the tmux server the test goes on to read.
func standUp(t *testing.T, resolve boxResolver, tmux session.Runner, repo, branch string) {
	t.Helper()
	mustRunSession(t, resolve, tmux, "start", "--repo", repo, "--branch", branch, "--detach")
}

// stopSession ends a session through the assembled command, which is how a
// test reaches the stopped half of the live probe.
func stopSession(t *testing.T, resolve boxResolver, tmux session.Runner, name string) {
	t.Helper()
	mustRunSession(t, resolve, tmux, "stop", name)
}

// mustRunSession drives one session subcommand as a fixture step and fails the
// test if it refuses.
func mustRunSession(t *testing.T, resolve boxResolver, tmux session.Runner, args ...string) {
	t.Helper()
	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), args...)
	if code != 0 {
		t.Fatalf("session %s exited %d (stderr: %s)", strings.Join(args, " "), code, stderr)
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
	standUp(t, resolve, tmux, "smith", "live-one")
	standUp(t, resolve, tmux, "smith", "spec-42")
	standUp(t, resolve, tmux, "web", "hotfix")
	stopSession(t, resolve, tmux, "smith-spec-42")
	stopSession(t, resolve, tmux, "web-hotfix")
	return resolve, tmux
}

// underRoot fails the test unless the named session verb is reachable at the
// surface an operator on the box types.
func underRoot(t *testing.T, verb string) {
	t.Helper()
	found, _, err := newRootCmd().Find([]string{"session", verb})
	if err != nil {
		t.Fatalf("Find(session %s) err = %v", verb, err)
	}
	if found.Name() != verb {
		t.Errorf("Find(session %s) = %q, want the %s command", verb, found.Name(), verb)
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
