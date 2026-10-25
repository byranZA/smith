package cli

import (
	"context"
	"slices"
	"strings"
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
	recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
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

func TestLoopRunRefusesAnUnavailableTaskBeforeAnyWorkBranchIsSetUp(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		issue string
		want  string
	}{
		"closed":      {taskJSON(80, "CLOSED", "ready-for-agent"), "#80 is closed\n"},
		"for a human": {taskJSON(80, "OPEN", "ready-for-human"), "#80 is meant for a human; pair on it with --interactive\n"},
		"unlabelled":  {taskJSON(80, "OPEN", "bug"), "#80 is not labelled \"ready-for-agent\"\n"},
		"blocked":     {taskJSON(80, "OPEN", "ready-for-agent", 79), "#80 is blocked by #79\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeTracker{issues: map[string]string{"80": tc.issue}}
			claude := &fakeAgent{gh: gh}
			var requests []workbench.Request
			recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
				requests = append(requests, req)
				return fakeBench(ctx, dir, req)
			}

			_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: recording}, "80")

			if code == 0 || !strings.HasSuffix(stderr, tc.want) || len(claude.handed) != 0 || len(requests) != 0 {
				t.Errorf("exit %d, stderr %q, handed %v, work branches set up for %+v; want non-zero ending %q, no agent run and no work branch", code, stderr, claude.handed, requests, tc.want)
			}
		})
	}
}

func TestLoopListShowsAnUnavailableTaskUnderItsState(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent", 79)}}

	stdout, stderr, code := runLoopList(t, gh, "80")

	want := defaultSettings +
		"task #80 Task 80\n" +
		"next: none\n" +
		"  blocked: #80 (by #79)\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 and\n%s", code, stderr, stdout, want)
	}
}

// specWithChildren is the tracker holding spec #42 with tasks #43 and #44, both available, #44 naming #42 as its parent.
func specWithChildren() *fakeTracker {
	child := strings.TrimSuffix(taskJSON(44, "OPEN", "ready-for-agent"), "}") +
		`,"url":"https://github.com/byranZA/smith/issues/44","parent":{"number":42,"title":"Spec: the loop","url":"https://github.com/byranZA/smith/issues/42"}}`
	return &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": child,
	}}
}

func TestLoopRunWorksAChildTaskAloneOnItsParentsWorkBranch(t *testing.T) {
	t.Parallel()
	gh := specWithChildren()
	claude := &fakeAgent{gh: gh}
	var requests []workbench.Request
	recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		requests = append(requests, req)
		return fakeBench(ctx, dir, req)
	}

	stdout, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: recording}, "44")

	want := defaultSettings + "task #44 complete\n" + workBranch
	wantRequests := []workbench.Request{{Number: 42, Title: "Spec: the loop"}}
	if code != 0 || !slices.Equal(claude.handed, []string{"44"}) || stdout != want || !slices.Equal(requests, wantRequests) {
		t.Errorf("exit %d, stderr %q, handed %v, work branches set up for %+v, stdout =\n%s\nwant 0, handed [44], %+v, and\n%s", code, stderr, claude.handed, requests, stdout, wantRequests, want)
	}
}

func TestLoopRunOfASpecAfterOneOfItsTasksWorksOnlyWhatRemains(t *testing.T) {
	t.Parallel()
	gh := specWithChildren()
	claude := &fakeAgent{gh: gh}
	if _, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "44"); code != 0 {
		t.Fatalf("loop run 44 exit %d, stderr %q", code, stderr)
	}

	stdout, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "42")

	want := defaultSettings + "spec #42 complete\n" + workBranch
	if code != 0 || !slices.Equal(claude.handed, []string{"44", "43"}) || stdout != want {
		t.Errorf("exit %d, stderr %q, handed %v, stdout =\n%s\nwant 0, handed [44 43], and\n%s", code, stderr, claude.handed, stdout, want)
	}
}

func TestLoopListOfAChildTaskShowsThatTaskAlone(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runLoopList(t, specWithChildren(), "44")

	want := defaultSettings +
		"task #44 Task 44\n" +
		"next: #44 Task 44\n" +
		"  available: #44\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 and\n%s", code, stderr, stdout, want)
	}
}

// parentLabelled is the tracker holding #42 labelled label, with available tasks #43 and #44 as its sub-issues.
func parentLabelled(label string) *fakeTracker {
	gh := specWithChildren()
	gh.issues["42"] = strings.Replace(gh.issues["42"], `"labels":[{"name":"spec"}]`, `"labels":[{"name":"`+label+`"}]`, 1)
	return gh
}

func TestLoopRunWorksAnIssueWithSubIssuesAsASpec(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"bug", "ready-for-agent"} {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			gh := parentLabelled(label)
			claude := &fakeAgent{gh: gh}

			stdout, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "42")

			want := defaultSettings + "spec #42 complete\n" + workBranch
			if code != 0 || !slices.Equal(claude.handed, []string{"43", "44"}) || stdout != want {
				t.Errorf("exit %d, stderr %q, handed %v, stdout =\n%s\nwant 0, handed [43 44], and\n%s", code, stderr, claude.handed, stdout, want)
			}
		})
	}
}

func TestLoopRunRefusesASpecWithNoTasks(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{
		"76": `{"number":76,"title":"Spec: Gadgets","state":"OPEN","body":"","labels":[{"name":"spec"},{"name":"ready-for-agent"}],"subIssues":{"nodes":[]}}`,
	}}
	claude := &fakeAgent{gh: gh}
	var requests []workbench.Request
	recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		requests = append(requests, req)
		return fakeBench(ctx, dir, req)
	}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: recording}, "76")

	if code == 0 || !strings.HasSuffix(stderr, "#76 has no tasks\n") || len(claude.handed) != 0 || len(requests) != 0 {
		t.Errorf("exit %d, stderr %q, handed %v, work branches set up for %+v; want non-zero ending %q, no agent run and no work branch", code, stderr, claude.handed, requests, "#76 has no tasks\n")
	}
}

func TestLoopListLeavesANestedSpecOutOfTheAvailableTasks(t *testing.T) {
	t.Parallel()
	gh := specWithChildren()
	gh.issues["44"] = strings.TrimSuffix(taskJSON(44, "OPEN", "ready-for-agent"), "}") + `,"subIssues":{"nodes":[{"number":90,"state":"OPEN"}]}}`

	stdout, stderr, code := runLoopList(t, gh, "42")

	want := defaultSettings +
		"spec #42 Spec: the loop\n" +
		"next: #43 Task 43\n" +
		"  available: #43\n" +
		"  not ready for an agent or a human: #44\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 and\n%s", code, stderr, stdout, want)
	}
}
