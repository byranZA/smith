package cli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/workbench"
)

func TestLoopRunInteractivePairsOnANamedHumanTaskAndPushesItOnceClosed(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-human")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	origin := &fakeOrigin{}
	w, _ := repoWith(t, gh, claude, "")
	w.pusher = origin.at

	stdout, stderr, code := runLoopRun(t, w, "80", "--interactive")

	want := defaultSettings + "task #80 closed\n" + "work branch: smith/80-task-80\n"
	pushed := []string{"smith/80-task-80 from " + rootOf(t, w)}
	attached := len(claude.commands) == 1 && claude.commands[0].Attached
	if code != 0 || !slices.Equal(claude.handed, []string{"80"}) || !attached || stdout != want || !slices.Equal(origin.pushed, pushed) {
		t.Errorf("exit %d, stderr %q, handed %v, commands %+v, pushed %q, stdout =\n%s\nwant 0, #80 to an attached agent, pushed %q, and\n%s", code, stderr, claude.handed, claude.commands, origin.pushed, stdout, pushed, want)
	}
}

func TestLoopRunInteractiveLeavesANamedHumanTaskLeftOpenToTheHuman(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-human")}}
	idle := &idleAgent{}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idle, lookPath: onPath}, "80", "--interactive")

	if code != 1 || idle.runs != 1 || stdout != defaultSettings+"task #80 still open\n"+"work branch: smith/80-task-80\n" {
		t.Errorf("exit %d after %d agent runs, stdout %q; want 1 after 1 run, reporting #80 still open", code, idle.runs, stdout)
	}
}

func TestLoopRunInteractivePairsOnAChildHumanTaskOnItsParentsWorkBranch(t *testing.T) {
	t.Parallel()
	gh := specWithChildren()
	gh.issues["44"] = strings.Replace(gh.issues["44"], "ready-for-agent", "ready-for-human", 1)
	claude := &fakeAgent{gh: gh}
	var requests []workbench.Request
	recording := func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		requests = append(requests, req)
		return fakeBench(ctx, dir, req)
	}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: recording}, "44", "--interactive")

	wantRequests := []workbench.Request{{Number: 42, Title: "Spec: the loop"}}
	if code != 0 || !slices.Equal(claude.handed, []string{"44"}) || !slices.Equal(requests, wantRequests) {
		t.Errorf("exit %d, stderr %q, handed %v, work branches set up for %+v; want 0, handed [44], and %+v", code, stderr, claude.handed, requests, wantRequests)
	}
}

func TestLoopRunInteractiveRefusesANamedHumanTasksMissingAgentBeforeSettingUpTheWorkBranch(t *testing.T) {
	t.Parallel()
	task := strings.Replace(presetTask(80, "OPEN", "codex"), "ready-for-agent", "ready-for-human", 1)
	gh := &fakeTracker{issues: map[string]string{"80": task}}
	idle := &idleAgent{}
	w, _ := repoWith(t, gh, idle, opusRepoFile)
	w.lookPath = installedOnly("claude")
	w, benches := countingBench(w)

	_, stderr, code := runLoopRun(t, w, "80", "--interactive")

	if want := "agent codex is not installed"; code == 0 || !strings.Contains(stderr, want) || *benches != 0 || idle.runs != 0 {
		t.Errorf("exit %d, stderr %q, %d work branches, %d agent runs; want non-zero naming %q, no work branch and no agent", code, stderr, *benches, idle.runs, want)
	}
}

func TestLoopRunInteractiveRefusesANamedTaskNotReadyToPairOn(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		issue string
		want  string
	}{
		"closed":     {taskJSON(80, "CLOSED", "ready-for-human"), "#80 is closed\n"},
		"blocked":    {taskJSON(80, "OPEN", "ready-for-human", 79), "#80 is blocked by #79\n"},
		"unlabelled": {taskJSON(80, "OPEN", "bug"), "#80 is not labelled \"ready-for-agent\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeTracker{issues: map[string]string{"80": tc.issue}}
			claude := &fakeAgent{gh: gh}
			w, benches := countingBench(loopWiring{gh: gh, launcher: claude, lookPath: onPath})

			_, stderr, code := runLoopRun(t, w, "80", "--interactive")

			if code != 1 || !strings.HasSuffix(stderr, tc.want) || len(claude.handed) != 0 || *benches != 0 {
				t.Errorf("exit %d, stderr %q, handed %v, %d work branches; want 1 ending %q, no agent run and no work branch", code, stderr, claude.handed, *benches, tc.want)
			}
		})
	}
}
