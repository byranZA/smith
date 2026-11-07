package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/staging"
)

// recordingStack fakes the stack a run on a box brings up and down.
type recordingStack struct{ ups, downs []string }

func (r *recordingStack) Ensure(_ context.Context, dir string, _ io.Writer) error {
	r.ups = append(r.ups, dir)
	return nil
}

func (r *recordingStack) Release(context.Context, io.Writer) error {
	if len(r.downs) >= len(r.ups) {
		return nil
	}
	r.downs = append(r.downs, "down")
	return nil
}

// onABox is a box resolver for a machine that has been set up as a box, with
// a workspace that holds no repos.
func onABox(t *testing.T) boxResolver {
	t.Helper()
	return resolvedBoxPlacing(t.TempDir(), "api")
}

// offABox is a box resolver for a machine nothing was staged on.
func offABox(inv hint.Invocation) (boxConfig, error) {
	return boxConfig{}, &staging.AbsentError{Path: "/var/lib/smith/blueprint.yaml", Hint: inv}
}

const stackFile = "stack:\n  up: make up\n  down: make down\n"

func TestLoopListOnABoxShowsTheStackOnFromTheRepoFile(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, stackFile)
	w.box = onABox(t)

	stdout, stderr, code := runLoopListIn(t, w, "42")

	if want := "push:   on (built-in default)\nstack:  on (repo file)\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 holding %q", code, stderr, stdout, want)
	}
}

func TestLoopListOnABoxWithNoStackShowsNoStackLine(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, "push: true\n")
	w.box = onABox(t)

	stdout, _, code := runLoopListIn(t, w, "42")

	if code != 0 || strings.Contains(stdout, "stack:") {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 and no stack line", code, stdout)
	}
}

func TestLoopListOffABoxShowsTheStackOffAndNamesStack(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, stackFile)
	w.box = offABox

	stdout, _, code := runLoopListIn(t, w, "42")

	if want := "stack:  off (--stack turns it on)\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 holding %q", code, stdout, want)
	}
}

func TestLoopListOffABoxWithNoStackShowsNoStackLine(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, "push: true\n")
	w.box = offABox

	stdout, _, code := runLoopListIn(t, w, "42", "--stack")

	if code != 0 || strings.Contains(stdout, "stack:") {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 and no stack line", code, stdout)
	}
}

func TestLoopListOffABoxWithStackShowsTheStackOn(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, stackFile)
	w.box = offABox

	stdout, _, code := runLoopListIn(t, w, "42", "--stack")

	if want := "stack:  on (repo file)\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 holding %q", code, stdout, want)
	}
}

func TestLoopRunOffABoxWithStackBringsTheStackUpAndDown(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = offABox
	stack := &recordingStack{}
	w.newStack = func(string, string, string) loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80", "--stack")

	if code != 0 || len(stack.ups) != 1 || len(stack.downs) != 1 || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want 0, one up, one down and the stack line", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOnABoxWithStackChangesNothing(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	stack := &recordingStack{}
	w.newStack = func(string, string, string) loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80", "--stack")

	if code != 0 || len(stack.ups) != 1 || len(stack.downs) != 1 || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want the same as without --stack", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOnABoxBringsTheDeclaredStackUpAndDown(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	stack := &recordingStack{}
	w.newStack = func(string, string, string) loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 1 || len(stack.downs) != 1 || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want 0, one up, one down and the stack line", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOffABoxRunsNoStack(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = offABox
	stack := &recordingStack{}
	w.newStack = func(string, string, string) loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 0 || len(stack.downs) != 0 || !strings.Contains(stdout, "stack:  off (--stack turns it on)\n") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want 0, no stack and the stack off line", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOnABoxWhoseCheckoutDeclaresNoStackStillLetsEnsureInspectEachWorktree(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "push: true\n")
	w.box = onABox(t)
	stack := &recordingStack{}
	w.newStack = func(string, string, string) loop.Stacker { return stack }

	_, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 1 {
		t.Errorf("exit %d, ups %v, stderr %q; want 0 and the worktree handed to the stack", code, stack.ups, stderr)
	}
}

// leaseHeldBy takes the stack lease key names in state for a run of branch in
// dir and returns until the test ends.
func leaseHeldBy(t *testing.T, state, key, branch, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte(stackFile), 0o644); err != nil {
		t.Fatal(err)
	}
	holder := &stack.Stack{Command: noopCommander{}, State: state, Key: key, Branch: branch}
	if err := holder.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Release(context.Background(), io.Discard) })
}

// noopCommander runs every stack command successfully.
type noopCommander struct{}

func (noopCommander) Exec(context.Context, string, string, string, io.Writer) error { return nil }

// keyedGit answers the origin probe of the lease key with origin and defers
// every other command to next.
type keyedGit struct {
	origin string
	next   repofile.Runner
}

func (g keyedGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 5 && args[2] == "remote" {
		_, err := fmt.Fprintln(stdout, g.origin)
		return err
	}
	return g.next.Run(ctx, name, args, stdin, stdout, stderr)
}

func TestLoopRunRefusesAStackAnotherRunHoldsBeforeMarkingAnyTask(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	state := t.TempDir()
	w.state = func() (string, error) { return state, nil }
	w.git = keyedGit{origin: "git@github.com:acme/api.git", next: w.git}
	w.newStack = func(string, string, string) loop.Stacker { return &recordingStack{} }
	leaseHeldBy(t, state, stack.Key("https://github.com/Acme/api", ""), "smith/9-other", t.TempDir())

	_, stderr, code := runLoopRun(t, w, "80")

	if code == 0 || !strings.Contains(stderr, "smith/9-other") {
		t.Errorf("exit %d, stderr %q; want non-zero naming the holder's branch", code, stderr)
	}
	for _, call := range gh.ran {
		if strings.HasPrefix(call, "gh issue edit") {
			t.Errorf("gh ran %q, want no task marked", call)
		}
	}
}

func TestLoopRunWithNoStackGoesOnBesideAHolder(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "push: true\n")
	w.box = onABox(t)
	state := t.TempDir()
	w.state = func() (string, error) { return state, nil }
	w.git = keyedGit{origin: "git@github.com:acme/api.git", next: w.git}
	leaseHeldBy(t, state, stack.Key("git@github.com:acme/api.git", ""), "smith/9-other", t.TempDir())

	_, stderr, code := runLoopRun(t, w, "80")

	if code != 0 {
		t.Errorf("exit %d, stderr %q; want a run with no stack to go on", code, stderr)
	}
}

func TestLoopRunOnABoxWithARepoRefusesAHeldStackCreatingNothing(t *testing.T) {
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	writeBareRepoWithRepoFiles(t, workspace, "api", stackFile, "unused", stackFile+"push: false\n")
	w.box = resolvedBox(workspace, "api")
	state := t.TempDir()
	w.state = func() (string, error) { return state, nil }
	key, err := w.stackLeaseKey(t.Context(), filepath.Join(workspace, "api", "repo.git"))
	if err != nil {
		t.Fatal(err)
	}
	leaseHeldBy(t, state, key, "smith/9-other", t.TempDir())

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "api")

	_, statErr := os.Stat(filepath.Join(workspace, "api", "worktrees"))
	if code == 0 || !strings.Contains(stderr, "smith/9-other") || len(tmux.calls) != 0 || !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("exit %d, stderr %q, tmux %q, worktrees %v; want non-zero naming the holder, no session and no worktree", code, stderr, tmux.calls, statErr)
	}
}
