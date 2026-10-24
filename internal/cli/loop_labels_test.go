package cli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
)

// configuredLabels is a repo file naming every triage label in place of its default.
const configuredLabels = "labels:\n  agent: go\n  human: stop\n  spec: epic\n"

// labelledSpec is the tracker holding spec #42 labelled epic, with #43 for the agent and #44 for a human, by the configured labels.
func labelledSpec() *fakeTracker {
	spec := strings.Replace(specJSON(43, 44), `"labels":[{"name":"spec"}]`, `"labels":[{"name":"epic"}]`, 1)
	return &fakeTracker{issues: map[string]string{
		"42": spec,
		"43": taskJSON(43, "OPEN", "go"),
		"44": taskJSON(44, "OPEN", "stop"),
	}}
}

// orderedAgent is a fakeAgent that also records each launch among the gh commands, so their order shows.
type orderedAgent struct {
	fakeAgent
}

func (o *orderedAgent) Launch(ctx context.Context, cmd agent.Command) error {
	o.gh.ran = append(o.gh.ran, "agent")
	return o.fakeAgent.Launch(ctx, cmd)
}

func TestLoopRunWorksTasksByTheConfiguredLabels(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	gh.labels = []string{"go", "stop", "epic"}
	claude := &fakeAgent{gh: gh}
	w, _ := repoWith(t, gh, claude, configuredLabels)

	stdout, stderr, code := runLoopRun(t, w, "42")

	wantTail := "spec #42 stopped: no task is available for an agent\n  waiting on a human: #44\n" + workBranch
	if code != 1 || !slices.Equal(claude.handed, []string{"43"}) || !strings.HasSuffix(stdout, wantTail) {
		t.Errorf("exit %d, handed %v, stdout %q, stderr %q; want 1, #43 alone handed out and a report ending %q", code, claude.handed, stdout, stderr, wantTail)
	}
}

func TestLoopRunWarnsOfEachSetLabelTheRepoLacksBeforeAnyAgentRuns(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	gh.labels = []string{"epic"}
	claude := &orderedAgent{fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, configuredLabels)

	_, stderr, _ := runLoopRun(t, w, "42")

	want := `smith: warning: labels.agent is "go", a label this repo does not have: no issue will match it, and smith never creates it` + "\n" +
		`smith: warning: labels.human is "stop", a label this repo does not have: smith will create it the first time it skips a task` + "\n"
	listed, launched := slices.Index(gh.ran, "gh label list --limit 1000 --json name"), slices.Index(gh.ran, "agent")
	if !strings.HasPrefix(stderr, want) || strings.Count(stderr, "labels.agent") != 1 || listed < 0 || launched < listed {
		t.Errorf("stderr %q, gh ran %q; want it to open with one warning per missing label\n%s\nand the labels read before any agent ran", stderr, gh.ran, want)
	}
}

func TestLoopRunWithDefaultLabelsChecksNoLabels(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "presets:\n  claude: {agent: claude}\nroles:\n  work: claude\n")

	_, stderr, code := runLoopRun(t, w, "42")

	for _, ran := range gh.ran {
		if strings.HasPrefix(ran, "gh label list") {
			t.Errorf("gh ran %q; want no label read with every label at its default", ran)
		}
	}
	if code != 0 || strings.Contains(stderr, "labels.") {
		t.Errorf("exit %d, stderr %q; want 0 and no label warning", code, stderr)
	}
}

func TestLoopRunCarriesOnWhenTheLabelCheckFails(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	gh.labelErr = "HTTP 502: Bad Gateway\n"
	claude := &fakeAgent{gh: gh}
	w, _ := repoWith(t, gh, claude, configuredLabels)

	_, stderr, _ := runLoopRun(t, w, "42")

	want := "smith: warning: could not check the repo has the labels the repo file sets: gh could not be used: HTTP 502: Bad Gateway\n"
	if !strings.HasPrefix(stderr, want) || !slices.Equal(claude.handed, []string{"43"}) {
		t.Errorf("stderr %q, handed %v; want it to open with %q and #43 still handed out", stderr, claude.handed, want)
	}
}

func TestLoopRunRefusesATaskWithoutTheConfiguredAgentLabelNamingIt(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}, labels: []string{"go"}}
	claude := &fakeAgent{gh: gh}
	w, _ := repoWith(t, gh, claude, "labels:\n  agent: go\n")

	_, stderr, code := runLoopRun(t, w, "80")

	want := "#80 is not labelled \"go\"\n"
	if code == 0 || !strings.HasSuffix(stderr, want) || len(claude.handed) != 0 {
		t.Errorf("exit %d, stderr %q, handed %v; want non-zero ending %q and no agent run", code, stderr, claude.handed, want)
	}
}

func TestLoopListReportsTheConfiguredLabels(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, "labels:\n  agent: go\n  spec: epic\n")

	stdout, stderr, code := runLoopListIn(t, w, "42")

	want := defaultSettings +
		"labels: agent go (repo file), human ready-for-human (built-in default), spec epic (repo file)\n" +
		"spec #42 Spec: the loop\n" +
		"next: #43 Task 43\n" +
		"  available: #43\n" +
		"  not ready for an agent or a human: #44\n"
	if code != 0 || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 and\n%s", code, stderr, stdout, want)
	}
}

func TestLoopRunRefusesTwoLabelsSharingANameBeforeAnyAgentRuns(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	idle := &idleAgent{}
	w, _ := repoWith(t, gh, idle, "labels:\n  human: ready-for-agent\n")

	_, stderr, code := runLoopRun(t, w, "42")

	want := `labels.human (repo file) is "ready-for-agent", the name labels.agent (built-in default) has`
	if code == 0 || !strings.Contains(stderr, want) || idle.runs != 0 || len(gh.ran) != 0 {
		t.Errorf("exit %d, stderr %q, %d agent runs, gh ran %v; want non-zero naming %q, no agent and no gh", code, stderr, idle.runs, gh.ran, want)
	}
}

func TestLoopRunRefusesALabelNamedForASmithMarkBeforeAnyAgentRuns(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	idle := &idleAgent{}
	w, _ := repoWith(t, gh, idle, "labels:\n  human: smith:working\n")

	_, stderr, code := runLoopRun(t, w, "42")

	want := `labels.human (repo file) is "smith:working", the label smith marks its own work with`
	if code == 0 || !strings.Contains(stderr, want) || idle.runs != 0 || len(gh.ran) != 0 {
		t.Errorf("exit %d, stderr %q, %d agent runs, gh ran %v; want non-zero naming %q, no agent and no gh", code, stderr, idle.runs, gh.ran, want)
	}
}
