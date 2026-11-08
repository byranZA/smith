package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/workbench"
)

func TestLoopRunWithNoCapWorksMoreThanTenAgentRuns(t *testing.T) {
	issues := map[string]string{}
	var numbers []int
	for n := 43; n < 58; n++ {
		numbers = append(numbers, n)
		issues[fmt.Sprint(n)] = taskJSON(n, "OPEN", "ready-for-agent")
	}
	issues["42"] = specJSON(numbers...)
	idle := &idleAgent{}

	stdout, _, _ := runLoopRun(t, loopWiring{gh: &fakeTracker{issues: issues}, launcher: idle, lookPath: onPath}, "42")

	if idle.runs != 30 || strings.Contains(stdout, "cap of") {
		t.Errorf("%d agent runs, stdout %q; want 30 runs (2 attempts of 15 tasks) and no cap reached", idle.runs, stdout)
	}
}

func TestLoopRunWithMaxIterationsZeroShowsNoCapFromTheFlag(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

	stdout, _, _ := runLoopRun(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", "--max-iterations", "0")

	if !strings.Contains(stdout, "cap:    none (--max-iterations)\n") {
		t.Errorf("stdout %q; want the cap line none (flag)", stdout)
	}
}

func TestLoopRunWithMaxIterationsShowsTheCapFromTheFlag(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

	stdout, _, _ := runLoopRun(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", "--max-iterations", "3")

	if !strings.Contains(stdout, "cap:    3 agent runs (--max-iterations)\n") {
		t.Errorf("stdout %q; want the cap line 3 (flag)", stdout)
	}
}

// threeTaskSpec is a spec of three open tasks for an agent.
func threeTaskSpec() *fakeTracker {
	return &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44, 45),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": taskJSON(44, "OPEN", "ready-for-agent"),
		"45": taskJSON(45, "OPEN", "ready-for-agent"),
	}}
}

func TestLoopRunStopsAtTheRepoFileCap(t *testing.T) {
	idle := &idleAgent{}
	w, _ := repoWith(t, threeTaskSpec(), idle, "limits:\n  agent_runs: 2\n")

	stdout, _, code := runLoopRun(t, w, "42")

	if code == 0 || idle.runs != 2 || !strings.Contains(stdout, "reached the cap of 2 agent runs") {
		t.Errorf("exit %d after %d agent runs, stdout =\n%s\nwant non-zero after 2 runs", code, idle.runs, stdout)
	}
}

func TestLoopRunShowsTheRepoFileAsTheCapOrigin(t *testing.T) {
	w, _ := repoWith(t, threeTaskSpec(), &idleAgent{}, "limits:\n  agent_runs: 2\n")

	stdout, _, _ := runLoopRun(t, w, "42")

	if !strings.Contains(stdout, "cap:    2 agent runs (repo file)\n") {
		t.Errorf("stdout =\n%s\nwant the cap line 2 agent runs (repo file)", stdout)
	}
}

func TestLoopRunFlagOverridesTheRepoFileCap(t *testing.T) {
	idle := &idleAgent{}
	w, _ := repoWith(t, threeTaskSpec(), idle, "limits:\n  agent_runs: 2\n")

	stdout, _, _ := runLoopRun(t, w, "42", "--max-iterations", "5")

	if idle.runs != 5 || !strings.Contains(stdout, "reached the cap of 5 agent runs") {
		t.Errorf("%d agent runs, stdout =\n%s\nwant the cap of 5 to stop the run", idle.runs, stdout)
	}
}

func TestLoopRunMaxIterationsZeroLiftsTheRepoFileCap(t *testing.T) {
	idle := &idleAgent{}
	w, _ := repoWith(t, threeTaskSpec(), idle, "limits:\n  agent_runs: 2\n")

	stdout, _, _ := runLoopRun(t, w, "42", "--max-iterations", "0")

	if idle.runs != 6 || strings.Contains(stdout, "cap of") {
		t.Errorf("%d agent runs, stdout =\n%s\nwant 6 runs (2 attempts of 3 tasks) and no cap reached", idle.runs, stdout)
	}
}

func TestLoopRunRefusesAnInvalidRepoFileCapNamingTheFileAndKey(t *testing.T) {
	idle := &idleAgent{}
	w, path := repoWith(t, threeTaskSpec(), idle, "limits:\n  agent_runs: 0\n")

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || idle.runs != 0 || !strings.Contains(stderr, path) || !strings.Contains(stderr, "limits.agent_runs") {
		t.Errorf("exit %d after %d runs, stderr %q; want a refusal naming %s and limits.agent_runs", code, idle.runs, stderr, path)
	}
}

func TestLoopRunGivesATaskTheRepoFileAttempts(t *testing.T) {
	idle := &idleAgent{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, idle, "limits:\n  task_attempts: 3\n")

	stdout, _, _ := runLoopRun(t, w, "42")

	if idle.runs != 3 || !strings.Contains(stdout, "attempts: 3 (repo file)\n") {
		t.Errorf("%d agent runs, stdout =\n%s\nwant 3 runs and attempts: 3 (repo file)", idle.runs, stdout)
	}
}

func TestLoopRunFlagOverridesTheRepoFileAttempts(t *testing.T) {
	idle := &idleAgent{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, idle, "limits:\n  task_attempts: 3\n")

	stdout, _, _ := runLoopRun(t, w, "42", "--max-attempts", "1")

	if idle.runs != 1 || !strings.Contains(stdout, "attempts: 1 (--max-attempts)\n") {
		t.Errorf("%d agent runs, stdout =\n%s\nwant 1 run and attempts: 1 (--max-attempts)", idle.runs, stdout)
	}
}

func TestLoopRunRefusesAnInvalidRepoFileAttemptsNamingTheFileAndKey(t *testing.T) {
	idle := &idleAgent{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, path := repoWith(t, gh, idle, "limits:\n  task_attempts: 0\n")

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || idle.runs != 0 || !strings.Contains(stderr, path) || !strings.Contains(stderr, "limits.task_attempts") {
		t.Errorf("exit %d after %d runs, stderr %q; want a refusal naming %s and limits.task_attempts", code, idle.runs, stderr, path)
	}
}

func TestLoopRunGivesTheReviewTheRepoFileAttempts(t *testing.T) {
	idle := &idleAgent{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "CLOSED", "ready-for-agent")}}
	w, _ := repoWith(t, gh, idle, opusRepoFile+"  review: codex\nlimits:\n  task_attempts: 3\n")
	findings := filepath.Join(t.TempDir(), "smith", "review-42.json")
	w.bench = func(_ context.Context, _ string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		return reviewingBench{benchAt: benchAt{branch: workbench.Branch(req.Number, req.Title)}, file: findings}, workbench.Lag{}, nil
	}

	stdout, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || idle.runs != 3 {
		t.Errorf("exit %d after %d agent runs, stdout =\n%s\nstderr %q; want non-zero after 3 review attempts", code, idle.runs, stdout, stderr)
	}
}
