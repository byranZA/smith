package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/repofile"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/workbench"
)

// recordingCommander fakes the process boundary a real stack reaches its
// commands through, keeping the step of each command it was asked to run.
type recordingCommander struct {
	mu    sync.Mutex
	steps []string
}

func (r *recordingCommander) Exec(_ context.Context, _, step, _ string, _ io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
	return nil
}

// ran returns the steps run so far, in order.
func (r *recordingCommander) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.steps)
}

// realWorktrees is a bench whose task worktrees are real directories holding
// the repo file, so a real stack can read what the repo declares.
type realWorktrees struct {
	benchAt
	root, repoFile string
}

func (b realWorktrees) Open(_ context.Context, task int) (string, error) {
	dir := filepath.Join(b.root, strconv.Itoa(task))
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte(b.repoFile), 0o644)
}

// realStackOn makes the wiring build a real stack over run, in worktrees that
// hold the repo file.
func realStackOn(t *testing.T, w *loopWiring, repoFile string, run stack.Commander) {
	t.Helper()
	root := t.TempDir()
	w.bench = func(_ context.Context, _ string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		return realWorktrees{benchAt: benchAt{branch: workbench.Branch(req.Number, req.Title)}, root: root, repoFile: repoFile}, workbench.Lag{}, nil
	}
	w.newStack = func(state, key, branch string) runStack {
		return &stack.Stack{Command: run, State: state, Key: key, Branch: branch}
	}
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
	run := &recordingCommander{}
	realStackOn(t, &w, stackFile, run)

	stdout, stderr, code := runLoopRun(t, w, "80", "--stack")

	if code != 0 || !slices.Equal(run.ran(), []string{"stack up", "stack down"}) || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ran %v, stdout %q, stderr %q; want 0, one up, one down and the stack line", code, run.ran(), stdout, stderr)
	}
}

func TestLoopRunOnABoxWithStackChangesNothing(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	run := &recordingCommander{}
	realStackOn(t, &w, stackFile, run)

	stdout, stderr, code := runLoopRun(t, w, "80", "--stack")

	if code != 0 || !slices.Equal(run.ran(), []string{"stack up", "stack down"}) || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ran %v, stdout %q, stderr %q; want the same as without --stack", code, run.ran(), stdout, stderr)
	}
}

func TestLoopRunOnABoxBringsTheDeclaredStackUpAndDown(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	run := &recordingCommander{}
	realStackOn(t, &w, stackFile, run)

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || !slices.Equal(run.ran(), []string{"stack up", "stack down"}) || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ran %v, stdout %q, stderr %q; want 0, one up, one down and the stack line", code, run.ran(), stdout, stderr)
	}
}

func TestLoopRunOffABoxRunsNoStack(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = offABox
	run := &recordingCommander{}
	realStackOn(t, &w, stackFile, run)

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(run.ran()) != 0 || !strings.Contains(stdout, "stack:  off (--stack turns it on)\n") {
		t.Errorf("exit %d, ran %v, stdout %q, stderr %q; want 0, no stack and the stack off line", code, run.ran(), stdout, stderr)
	}
}

// leftBehind sets up a repo whose previous run died holding the stack lease
// with a down command recorded, so a run that recovers runs "stack down".
func leftBehind(t *testing.T, w *loopWiring) {
	t.Helper()
	state := t.TempDir()
	w.state = func() (string, error) { return state, nil }
	w.git = keyedGit{origin: "git@github.com:acme/api.git", next: w.git}
	record := stack.Record{Dir: t.TempDir(), Branch: "smith/9-other", Up: "make up", Down: "make down"}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	leases := filepath.Join(state, "stacks")
	if err := os.MkdirAll(leases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leases, stack.Key("git@github.com:acme/api.git", "")+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoopRunOffABoxWithoutStackRecoversNothing(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = offABox
	run := &recordingCommander{}
	realStackOn(t, &w, stackFile, run)
	leftBehind(t, &w)

	_, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(run.ran()) != 0 {
		t.Errorf("exit %d, ran %v, stderr %q; want 0 and no recovery", code, run.ran(), stderr)
	}
}

func TestLoopRunOffABoxWithStackRecoversTheStackALeftBehindOnceEvenThoughTheRepoDeclaresNone(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "push: true\n")
	w.box = offABox
	run := &recordingCommander{}
	realStackOn(t, &w, "push: true\n", run)
	leftBehind(t, &w)

	_, stderr, code := runLoopRun(t, w, "80", "--stack")

	if want := []string{"stack down"}; code != 0 || !slices.Equal(run.ran(), want) {
		t.Errorf("exit %d, ran %v, stderr %q; want 0 and %v", code, run.ran(), stderr, want)
	}
}

func TestLoopRunOnABoxWhoseCheckoutDeclaresNoStackRunsNoStackCommand(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "push: true\n")
	w.box = onABox(t)
	run := &recordingCommander{}
	realStackOn(t, &w, "push: true\n", run)

	_, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(run.ran()) != 0 {
		t.Errorf("exit %d, ran %v, stderr %q; want 0 and no stack command", code, run.ran(), stderr)
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
	t.Cleanup(func() {
		if err := holder.Release(context.Background(), io.Discard); err != nil {
			t.Errorf("Release() error = %v", err)
		}
	})
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
	realStackOn(t, &w, stackFile, noopCommander{})
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

// originGit fakes git in a repo whose origin probe fails with probeErr and whose remotes are remotes.
type originGit struct {
	probeErr error
	remotes  string
}

func (g originGit) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	switch args[2] {
	case "remote":
		if len(args) == 5 {
			return g.probeErr
		}
		_, err := fmt.Fprintln(stdout, g.remotes)
		return err
	default:
		_, err := fmt.Fprintln(stdout, "/repo/.git")
		return err
	}
}

func TestStackLeaseKeyFailsWhenAnExistingOriginCannotBeRead(t *testing.T) {
	t.Parallel()
	w := loopWiring{git: originGit{probeErr: errors.New("boom"), remotes: "origin"}}
	if key, err := w.stackLeaseKey(t.Context(), "/repo"); err == nil {
		t.Errorf("stackLeaseKey() = %q, nil, want an error", key)
	}
}

func TestStackLeaseKeyFallsBackToTheGitDirectoryWhenThereIsNoOrigin(t *testing.T) {
	t.Parallel()
	w := loopWiring{git: originGit{probeErr: errors.New("no such remote"), remotes: "upstream"}}
	got, err := w.stackLeaseKey(t.Context(), "/repo")
	if want := stack.Key("", "/repo/.git"); err != nil || got != want {
		t.Errorf("stackLeaseKey() = %q, %v, want %q", got, err, want)
	}
}
