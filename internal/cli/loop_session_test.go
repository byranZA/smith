package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/misetrust"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
)

// boxMise stands in for mise on a box, recording the argv it was handed and
// finding under mise exec only the programs in installed.
type boxMise struct {
	installed []string
	calls     [][]string
}

func (m *boxMise) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	m.calls = append(m.calls, append([]string{name}, args...))
	program := args[len(args)-1]
	if !slices.Contains(m.installed, program) {
		return errors.New("exit status 1")
	}
	_, err := io.WriteString(stdout, "/home/smith/.local/share/mise/shims/"+program+"\n")
	return err
}

// boxGh stands in for gh in a clone on a box, recording the argv it was
// handed: gh is found only under mise exec, as the relayed PATH lacks it.
type boxGh struct {
	tracker *fakeTracker
	calls   [][]string
}

func (g *boxGh) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	g.calls = append(g.calls, append([]string{name}, args...))
	argv := untrusted(append([]string{name}, args...))
	name, args = argv[0], argv[1:]
	if filepath.Base(name) != "mise" || len(args) < 3 || args[0] != "exec" || args[1] != "--" {
		return fmt.Errorf("exec: %q: executable file not found in $PATH", name)
	}
	return g.tracker.Run(ctx, args[2], args[3:], stdin, stdout, stderr)
}

// sessionLoop is loop wiring on a box whose workspace holds the bare clone of
// "smith", driving the tmux server tmux, where mise finds claude and codex and
// the relayed PATH finds no agent at all.
func sessionLoop(t *testing.T, gh *fakeTracker, tmux *tmuxBox) (loopWiring, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	return loopWiring{
		gh:       gh,
		ghIn:     func(string) tracker.Runner { return &boxGh{tracker: gh} },
		launcher: &idleAgent{},
		lookPath: offPath,
		mise:     &boxMise{installed: []string{"claude", "codex"}},
		git:      connection.System(),
		tmux:     tmux,
		box:      resolvedBox(workspace, "smith"),
		root:     t.TempDir(),
	}, workspace
}

// readySpec is a tracker holding spec #42, "Spec: the loop", with task #43 ready for an agent.
func readySpec() *fakeTracker {
	return &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
}

// started returns the tmux new-session argv tmux was handed.
func started(tmux *tmuxBox) [][]string {
	var calls [][]string
	for _, c := range tmux.calls {
		if len(c) > 1 && c[1] == "new-session" {
			calls = append(calls, c)
		}
	}
	return calls
}

func TestLoopRunOnABoxStartsASessionWhoseRootProcessIsTheLoop(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith", "--max-iterations", "3", "--agent", "codex")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "smith-42-the-loop")
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-smith-42-the-loop", "-c", dir, "sh", "-c", misetrust.Script, filepath.Join(workspace, "smith"), "mise", "exec", "--",
		"/usr/local/bin/smith", "loop", "run", "42", "--agent=codex", "--max-iterations=3", "--branch=smith/42-the-loop", "--in-session"}}
	if got := started(tmux); !reflect.DeepEqual(got, want) {
		t.Errorf("tmux new-session = %q, want %q", got, want)
	}
}

func TestLoopRunOnABoxReadsTheIssueWithGhUnderMiseExec(t *testing.T) {
	gh := &boxGh{tracker: readySpec()}
	w, workspace := sessionLoop(t, gh.tracker, &tmuxBox{})
	var dirs []string
	w.ghIn = func(dir string) tracker.Runner {
		dirs = append(dirs, dir)
		return gh
	}

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
	}
	want := []string{"sh", "-c", misetrust.Script, filepath.Join(workspace, "smith"), "mise", "exec", "--", "gh", "issue", "view", "42"}
	if len(gh.calls) == 0 || !reflect.DeepEqual(gh.calls[0][:len(want)], want) {
		t.Errorf("gh ran %q, want a first call beginning %q", gh.calls, want)
	}
	if clone := filepath.Join(workspace, "smith", "repo.git"); len(dirs) == 0 || dirs[0] != clone {
		t.Errorf("gh ran in %q, want %q", dirs, clone)
	}
}

func TestLoopRunOnABoxNamesTheSessionAndHowToWatchIt(t *testing.T) {
	w, _ := sessionLoop(t, readySpec(), &tmuxBox{})

	stdout, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	want := "session smith-smith-42-the-loop is live on branch smith/42-the-loop of smith, driven by the loop\n" +
		"attach: smith session attach smith-smith-42-the-loop\n" +
		"list:   smith session list\n"
	if code != 0 || !strings.HasSuffix(stdout, want) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and stdout ending %q", code, stdout, stderr, want)
	}
}

func TestLoopRunOnABoxRelayedNamesTheBoxInTheCommandsItPrints(t *testing.T) {
	w, _ := sessionLoop(t, readySpec(), &tmuxBox{})
	root := &cobra.Command{Use: "smith", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String(relayedBoxFlag, "", "")
	root.AddCommand(newLoopCmd(inRepo(t, w)))
	root.SetArgs([]string{"loop", "run", "42", "--repo", "smith", "--" + relayedBoxFlag, "dev"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	code := codeFromError(root.Execute())
	stdout, stderr := out.String(), errBuf.String()

	if code != 0 || !strings.HasSuffix(stdout, "attach: smith session attach dev smith-smith-42-the-loop\nlist:   smith session list dev\n") {
		t.Errorf("exit %d, stdout %q, stderr %q; want the commands spelled for box dev", code, stdout, stderr)
	}
}

func TestLoopRunOnABoxOnAChosenBranchNamesTheSessionAfterIt(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith", "--branch", "feat/x")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
	}
	dir := filepath.Join(workspace, "smith", "worktrees", "feat-x")
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-feat-x", "-c", dir, "sh", "-c", misetrust.Script, filepath.Join(workspace, "smith"), "mise", "exec", "--",
		"/usr/local/bin/smith", "loop", "run", "42", "--branch=feat/x", "--in-session"}}
	if got := started(tmux); !reflect.DeepEqual(got, want) {
		t.Errorf("tmux new-session = %q, want %q", got, want)
	}
}

func TestLoopRunOnABoxRefusesALiveSessionOnTheWorkBranch(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	standUp(t, resolvedBox(workspace, "smith"), tmux, "smith", "smith/42-the-loop")
	before := len(started(tmux))

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	want := "session smith-smith-42-the-loop is live on branch smith/42-the-loop: watch it with `smith session attach smith-smith-42-the-loop`, or stop it with `smith session stop smith-smith-42-the-loop` and run again"
	if code == 0 || !strings.Contains(stderr, want) || len(started(tmux)) != before {
		t.Errorf("exit %d, stderr %q, new sessions %d; want non-zero naming the live session and none started", code, stderr, len(started(tmux))-before)
	}
}

func TestLoopRunOnABoxReusesAStoppedSessionsWorktree(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, tmux, "smith", "smith/42-the-loop")
	dir := filepath.Join(workspace, "smith", "worktrees", "smith-42-the-loop")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("by hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopSession(t, resolve, tmux, "smith-smith-42-the-loop")

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	if code != 0 || !tmux.live["smith/smith-smith-42-the-loop"] {
		t.Fatalf("exit %d, stderr %q; want 0 and the session live again", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("the stopped session's worktree was not reused: %v", err)
	}
}

func TestLoopRunOnABoxThatFailsItsPreflightStartsNothing(t *testing.T) {
	tests := []struct {
		name   string
		issue  string
		repo   string
		gh     *fakeTracker
		onMise []string
		want   string
	}{
		{"agent not installed", "42", "smith", readySpec(), nil, "agent claude is not installed"},
		{"issue unreadable", "999", "smith", readySpec(), []string{"claude"}, "#999"},
		{"spec with no tasks", "42", "smith", &fakeTracker{issues: map[string]string{"42": specJSON()}}, []string{"claude"}, "#42 has no tasks"},
		{"task for a human", "43", "smith", &fakeTracker{issues: map[string]string{"43": taskJSON(43, "OPEN", "ready-for-human")}}, []string{"claude"}, "#43 is meant for a human"},
		{"repo not on the box", "42", "nope", readySpec(), []string{"claude"}, `repo "nope" is not declared`},
		{"tracker unreachable", "42", "smith", &fakeTracker{stderr: "gh: To get started with GitHub CLI, please run:  gh auth login\n"}, []string{"claude"}, "gh auth login"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmux := &tmuxBox{}
			w, workspace := sessionLoop(t, tc.gh, tmux)
			w.mise = &boxMise{installed: tc.onMise}

			_, stderr, code := runLoopRun(t, w, tc.issue, "--repo", tc.repo)

			_, statErr := os.Stat(filepath.Join(workspace, "smith", "worktrees"))
			if code == 0 || !strings.Contains(stderr, tc.want) || len(tmux.calls) != 0 || !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("exit %d, stderr %q, tmux %q, worktrees %v; want non-zero naming %q, no session and no worktree", code, stderr, tmux.calls, statErr, tc.want)
			}
		})
	}
}

func TestLoopRunWithARepoOffABoxIsRefused(t *testing.T) {
	tmux := &tmuxBox{}
	w, _ := sessionLoop(t, readySpec(), tmux)
	w.box = func(inv hint.Invocation) (boxConfig, error) {
		return boxConfig{}, &staging.AbsentError{Path: "/var/lib/smith/blueprint.yaml", Hint: inv}
	}

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	want := "--repo names a repo on a box, and smith is not running on one: run `smith loop run 42` from inside the repo here"
	if code == 0 || !strings.Contains(stderr, want) || len(tmux.calls) != 0 {
		t.Errorf("exit %d, stderr %q; want non-zero with %q", code, stderr, want)
	}
}

func TestLoopRunInteractiveWithARepoIsRefused(t *testing.T) {
	tmux := &tmuxBox{}
	w, _ := sessionLoop(t, readySpec(), tmux)

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith", "--interactive")

	want := "--interactive cannot run in a loop-driven session: start a human session with `smith session start --repo smith --branch <branch>` and run `smith loop run 42 --interactive` in it"
	if code == 0 || !strings.Contains(stderr, want) || len(tmux.calls) != 0 {
		t.Errorf("exit %d, stderr %q; want non-zero with %q", code, stderr, want)
	}
}

func TestLoopRunWithoutARepoRunsInTheForegroundAndStartsNoSession(t *testing.T) {
	tmux := &tmuxBox{}
	gh := readySpec()
	claude := &fakeAgent{gh: gh}
	w := loopWiring{gh: gh, launcher: claude, lookPath: onPath, mise: &boxMise{}, tmux: tmux}

	stdout, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || !reflect.DeepEqual(claude.handed, []string{"43"}) || !strings.Contains(stdout, "spec #42 complete") || len(tmux.calls) != 0 {
		t.Errorf("exit %d, handed %v, stdout %q, stderr %q, tmux %q; want the loop run here and no session", code, claude.handed, stdout, stderr, tmux.calls)
	}
}

func TestLoopRunOnABoxResolvesSettingsFromTheFetchedBaseTheBranchIsCutFrom(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	upstream := gitStdout(t, filepath.Join(workspace, "smith", "repo.git"), "remote", "get-url", "origin")
	writeRepoFile(t, upstream, "repo.yaml", "agent: codex\n")
	runGit(t, upstream, "add", ".smith")
	runGit(t, upstream, "commit", "-m", "repo file")
	w.mise = &boxMise{installed: []string{"claude"}}

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	_, statErr := os.Stat(filepath.Join(workspace, "smith", "worktrees"))
	if code == 0 || !strings.Contains(stderr, "agent codex is not installed") || len(tmux.calls) != 0 || !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("exit %d, stderr %q, tmux %q, worktrees %v; want non-zero naming the agent the fetched base sets, no session and no worktree", code, stderr, tmux.calls, statErr)
	}
}

func TestLoopRunOnABoxResumingACheckoutPreflightsWhatTheCheckoutHolds(t *testing.T) {
	tests := []struct {
		name, file, content, want string
	}{
		{"its repo file", "repo.yaml", "agent: codex\n", "agent codex is not installed"},
		{"its loop prompt", "prompt.md", "work {{NOPE}}\n", "unknown placeholder {{NOPE}}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmux := &tmuxBox{}
			w, workspace := sessionLoop(t, readySpec(), tmux)
			resolve := resolvedBox(workspace, "smith")
			standUp(t, resolve, tmux, "smith", "smith/42-the-loop")
			stopSession(t, resolve, tmux, "smith-smith-42-the-loop")
			writeRepoFile(t, filepath.Join(workspace, "smith", "worktrees", "smith-42-the-loop"), tc.file, tc.content)
			w.mise = &boxMise{installed: []string{"claude"}}
			before := len(started(tmux))

			_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

			if code == 0 || !strings.Contains(stderr, tc.want) || len(started(tmux)) != before {
				t.Errorf("exit %d, stderr %q, new sessions %d; want non-zero naming %q and none started", code, stderr, len(started(tmux))-before, tc.want)
			}
		})
	}
}

// writeRepoFile writes content to the named file under dir's .smith directory.
func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoopRunOnABoxFindsAnAgentOnlyMiseHasAndStartsTheSession(t *testing.T) {
	tmux := &tmuxBox{}
	w, _ := sessionLoop(t, readySpec(), tmux)

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	if code != 0 || len(started(tmux)) != 1 {
		t.Errorf("exit %d, stderr %q, sessions started %d; want 0 and one session for an agent only mise finds", code, stderr, len(started(tmux)))
	}
}

func TestLoopRunOnABoxLooksTheAgentUpUnderTheMiseItsSessionRunsUnder(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	installed := filepath.Join(os.Getenv("HOME"), ".local", "bin", "mise")
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, tmux, "smith", "smith/42-the-loop")
	stopSession(t, resolve, tmux, "smith-smith-42-the-loop")
	mise := &boxMise{installed: []string{"claude"}}
	w.mise = mise

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	launches := started(tmux)
	if code != 0 || len(mise.calls) != 1 || len(launches) == 0 {
		t.Fatalf("exit %d, stderr %q, mise %q, tmux %q; want 0, one lookup and a launch", code, stderr, mise.calls, launches)
	}
	dir := realDir(t, filepath.Join(workspace, "smith", "worktrees", "smith-42-the-loop"))
	lookup, launch := mise.calls[0], launches[len(launches)-1]
	trust := realDir(t, filepath.Join(workspace, "smith"))
	if got, want := []string{realDir(t, lookup[3]), lookup[4], lookup[5], realDir(t, lookup[6])}, []string{trust, installed, "-C", dir}; !reflect.DeepEqual(got, want) {
		t.Errorf("agent lookup ran %q, want it to start trusting %q with %q", lookup, trust, want[1:])
	}
	if got, want := []string{realDir(t, launch[6]), realDir(t, launch[10]), launch[11], launch[12]}, []string{dir, trust, installed, "exec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("session launch ran %q, want it in %q under %q", launch, dir, installed)
	}
}

// realDir is dir with its symlinks resolved, as git reports a worktree.
func realDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
