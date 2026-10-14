package cli

import (
	"context"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/workbench"
)

// standaloneTask is the tracker holding #80, an available task with no parent and no sub-issues.
func standaloneTask() *fakeTracker {
	return &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
}

func TestLoopRunWorksAStandaloneTaskAloneOnItsOwnWorkBranch(t *testing.T) {
	t.Parallel()
	gh := standaloneTask()
	claude := &fakeAgent{gh: gh}
	origin := &fakeOrigin{}
	w, _ := repoWith(t, gh, claude, "")
	w.pusher = origin.at

	stdout, stderr, code := runLoopRun(t, w, "80")

	want := defaultSettings + "task #80 complete\n" + "work branch: smith/80-task-80\n"
	pushed := []string{"smith/80-task-80 from " + rootOf(t, w)}
	if code != 0 || !slices.Equal(claude.handed, []string{"80"}) || stdout != want || !slices.Equal(origin.pushed, pushed) {
		t.Errorf("exit %d, stderr %q, handed %v, pushed %q, stdout =\n%s\nwant 0, handed [80], pushed %q, and\n%s", code, stderr, claude.handed, origin.pushed, stdout, pushed, want)
	}
}

func TestLoopRunFailsAStandaloneTaskSkippedAfterItsAttempts(t *testing.T) {
	t.Parallel()
	idle := &idleAgent{}

	stdout, _, code := runLoopRun(t, loopWiring{gh: standaloneTask(), launcher: idle, lookPath: onPath}, "80", "--max-attempts", "2")

	want := defaultSettings + "task #80 stopped: the agent left a task open on every attempt\n" +
		"  skipped after 2 attempts: #80\n" +
		"  #80 left in /bench/80\n" +
		"work branch: smith/80-task-80\n"
	if code == 0 || idle.runs != 2 || stdout != want {
		t.Errorf("exit %d after %d agent runs, stdout =\n%s\nwant non-zero after 2 runs, and\n%s", code, idle.runs, stdout, want)
	}
}

func TestLoopRunSetsUpAStandaloneTaskOnTheBranchFlagNames(t *testing.T) {
	t.Parallel()
	gh := standaloneTask()
	var requests []workbench.Request
	recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, error) {
		requests = append(requests, req)
		return fakeBench(ctx, dir, req)
	}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, bench: recording}, "80", "--branch", "fix/x")

	want := []workbench.Request{{Number: 80, Title: "Task 80", Branch: "fix/x"}}
	if code != 0 || !slices.Equal(requests, want) {
		t.Errorf("exit %d, stderr %q, work branches set up for %+v; want 0 and %+v", code, stderr, requests, want)
	}
}

func TestLoopListNamesAnAvailableStandaloneTaskAsNextAndNothingElse(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runLoopList(t, standaloneTask(), "80")

	want := defaultSettings +
		"task #80 Task 80\n" +
		"next: #80 Task 80\n" +
		"  available: #80\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 and\n%s", code, stderr, stdout, want)
	}
}
