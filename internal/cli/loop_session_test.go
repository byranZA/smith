package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
)

// sessionLoop is loop wiring on a box whose workspace holds the bare clone of "smith", driving the tmux server tmux.
func sessionLoop(t *testing.T, gh *fakeTracker, tmux *tmuxBox) (loopWiring, string) {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	return loopWiring{
		gh:       gh,
		ghIn:     func(string) tracker.Runner { return gh },
		launcher: &idleAgent{},
		lookPath: onPath,
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
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-smith-42-the-loop", "-c", dir,
		"/usr/local/bin/smith", "loop", "run", "42", "--agent=codex", "--max-iterations=3", "--branch=smith/42-the-loop"}}
	if got := started(tmux); !reflect.DeepEqual(got, want) {
		t.Errorf("tmux new-session = %q, want %q", got, want)
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
	want := [][]string{{"tmux", "new-session", "-d", "-s", "smith/smith-feat-x", "-c", dir,
		"/usr/local/bin/smith", "loop", "run", "42", "--branch=feat/x"}}
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
		name     string
		issue    string
		repo     string
		gh       *fakeTracker
		lookPath func(string) (string, error)
		want     string
	}{
		{"agent not installed", "42", "smith", readySpec(), func(string) (string, error) { return "", errors.New("not found") }, "agent claude is not installed"},
		{"issue unreadable", "999", "smith", readySpec(), onPath, "#999"},
		{"spec with no tasks", "42", "smith", &fakeTracker{issues: map[string]string{"42": specJSON()}}, onPath, "#42 has no tasks"},
		{"task for a human", "43", "smith", &fakeTracker{issues: map[string]string{"43": taskJSON(43, "OPEN", "ready-for-human")}}, onPath, "#43 is meant for a human"},
		{"repo not on the box", "42", "nope", readySpec(), onPath, `repo "nope" is not declared`},
		{"tracker unreachable", "42", "smith", &fakeTracker{stderr: "gh: To get started with GitHub CLI, please run:  gh auth login\n"}, onPath, "gh auth login"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmux := &tmuxBox{}
			w, workspace := sessionLoop(t, tc.gh, tmux)
			w.lookPath = tc.lookPath

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
	w := loopWiring{gh: gh, launcher: claude, lookPath: onPath, tmux: tmux}

	stdout, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || !reflect.DeepEqual(claude.handed, []string{"43"}) || !strings.Contains(stdout, "spec #42 complete") || len(tmux.calls) != 0 {
		t.Errorf("exit %d, handed %v, stdout %q, stderr %q, tmux %q; want the loop run here and no session", code, claude.handed, stdout, stderr, tmux.calls)
	}
}

func TestLoopRunOnABoxResolvesSettingsFromTheReposCommittedRepoFile(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	work := t.TempDir()
	runGit(t, work, "clone", filepath.Join(workspace, "smith", "repo.git"), ".")
	if err := os.MkdirAll(filepath.Join(work, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".smith", "repo.yaml"), []byte("agent: codex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".smith")
	runGit(t, work, "-c", "user.email=smith@example.com", "-c", "user.name=smith", "commit", "-m", "repo file")
	runGit(t, work, "push", "origin", "main")
	w.lookPath = func(name string) (string, error) {
		if name == "codex" {
			return "", errors.New("not found")
		}
		return onPath(name)
	}

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	if code == 0 || !strings.Contains(stderr, "agent codex is not installed") || len(tmux.calls) != 0 {
		t.Errorf("exit %d, stderr %q; want non-zero naming the agent the repo file sets", code, stderr)
	}
}
