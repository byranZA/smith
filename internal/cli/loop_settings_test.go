package cli

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLoopListReportsAnUntouchedStarterAsClaudeWithItsOwnDefaults(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "CLOSED", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &idleAgent{}, "")
	home := t.TempDir()
	if _, _, code := runRepoInit(t, rootOf(t, w), rootOf(t, w), home); code != 0 {
		t.Fatalf("repo init exit %d", code)
	}

	stdout, stderr, code := runLoopListIn(t, w, "42")

	want := "preset: claude (repo file)\n" +
		"agent:  claude (repo file)\n" +
		"model:  the agent's own default (repo file)\n" +
		"effort: the agent's own default (repo file)\n" +
		"push:   on (built-in default)\n"
	if code != 0 || !strings.HasPrefix(stdout, want) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and the settings\n%s", code, stdout, stderr, want)
	}
}

func TestLoopRunReportsEachSettingWithItsOrigin(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, opusRepoFile)

	stdout, stderr, code := runLoopRun(t, w, "42", "--effort", "low")

	wantReport := "preset: opus (repo file)\n" +
		"agent:  claude (repo file)\n" +
		"model:  opus (repo file)\n" +
		"effort: low (flag)\n"
	wantArgs := []string{"--model", "opus", "--effort", "low"}
	if code != 0 || !strings.HasPrefix(stdout, wantReport) || len(claude.commands) != 1 || !containsRun(claude.commands[0].Args, wantArgs) {
		t.Errorf("exit %d, stdout %q, stderr %q, ran %v; want 0, the report\n%s\nand claude given %q", code, stdout, stderr, claude.commands, wantReport, wantArgs)
	}
}

func TestLoopRunFlagOverridesTheRepoFileForOneRunAndLeavesItUnchanged(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	content := opusRepoFile
	w, path := repoWith(t, gh, claude, content)

	stdout, _, code := runLoopRun(t, w, "42", "--model", "sonnet")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || !strings.Contains(stdout, "model:  sonnet (flag)\n") || len(claude.commands) != 1 || !containsRun(claude.commands[0].Args, []string{"--model", "sonnet"}) || string(after) != content {
		t.Errorf("exit %d, stdout %q, ran %v, repo file %q; want sonnet from a flag and the repo file unchanged", code, stdout, claude.commands, after)
	}
}

const opusRepoFile = "presets:\n" +
	"  opus:  {agent: claude, model: opus, effort: medium}\n" +
	"  codex: {agent: codex, model: gpt-6.1-sol, effort: high}\n" +
	"roles:\n" +
	"  work: opus\n"

func TestLoopRunHandsEachTaskToTheWorkPreset(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, opusRepoFile)

	_, stderr, code := runLoopRun(t, w, "42")

	want := []string{"--model", "opus", "--effort", "medium"}
	if code != 0 || len(claude.commands) != 2 || claude.commands[0].Name != "claude" || !containsRun(claude.commands[0].Args, want) ||
		claude.commands[1].Name != "claude" || !containsRun(claude.commands[1].Args, want) {
		t.Errorf("exit %d, stderr %q, ran %v; want both tasks handed to claude given %q", code, stderr, claude.commands, want)
	}
}

func TestLoopRunWithAnAgentFlagRunsAOneOffPresetWithoutTheRepoFilesModel(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	codex := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, codex, opusRepoFile)

	stdout, stderr, code := runLoopRun(t, w, "42", "--agent", "codex")

	wantReport := "preset: one-off (flag)\n" +
		"agent:  codex (flag)\n" +
		"model:  the agent's own default (flag)\n" +
		"effort: the agent's own default (flag)\n"
	if code != 0 || !strings.HasPrefix(stdout, wantReport) || len(codex.commands) != 1 || codex.commands[0].Name != "codex" ||
		slices.Contains(codex.commands[0].Args, "--model") || slices.Contains(codex.commands[0].Args, "opus") {
		t.Errorf("exit %d, stdout %q, stderr %q, ran %v; want 0, the report\n%s\nand codex given no model", code, stdout, stderr, codex.commands, wantReport)
	}
}

func TestLoopRunWithAgentAndModelFlagsRunsAOneOffPresetOfBoth(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	pi := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, pi, opusRepoFile)

	_, stderr, code := runLoopRun(t, w, "42", "--agent", "pi", "--model", "sonnet")

	if code != 0 || len(pi.commands) != 1 || pi.commands[0].Name != "pi" || !containsRun(pi.commands[0].Args, []string{"--model", "sonnet"}) {
		t.Errorf("exit %d, stderr %q, ran %v; want pi given model sonnet", code, stderr, pi.commands)
	}
}

func TestLoopRunWithAModelFlagAloneKeepsTheWorkPresetsAgentAndEffort(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, opusRepoFile)

	_, stderr, code := runLoopRun(t, w, "42", "--model", "sonnet")

	want := []string{"--model", "sonnet", "--effort", "medium"}
	if code != 0 || len(claude.commands) != 1 || claude.commands[0].Name != "claude" || !containsRun(claude.commands[0].Args, want) {
		t.Errorf("exit %d, stderr %q, ran %v; want claude given %q", code, stderr, claude.commands, want)
	}
}

func TestLoopRunWithNoModelGivesTheAgentNone(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, "")

	_, _, code := runLoopRun(t, w, "42")

	if code != 0 || len(claude.commands) != 1 || slices.Contains(claude.commands[0].Args, "--model") || slices.Contains(claude.commands[0].Args, "--effort") {
		t.Errorf("exit %d, ran %v; want claude given no model and no effort", code, claude.commands)
	}
}

func TestLoopRunWithAPresetFlagHandsEveryTaskToThatPreset(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	codex := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, codex, opusRepoFile)

	stdout, stderr, code := runLoopRun(t, w, "42", "--preset", "codex")

	wantReport := "preset: codex (flag)\n" +
		"agent:  codex (flag)\n" +
		"model:  gpt-6.1-sol (flag)\n" +
		"effort: high (flag)\n"
	want := []string{"--model", "gpt-6.1-sol"}
	if code != 0 || !strings.HasPrefix(stdout, wantReport) || len(codex.commands) != 2 || codex.commands[0].Name != "codex" || !containsRun(codex.commands[0].Args, want) ||
		codex.commands[1].Name != "codex" || !containsRun(codex.commands[1].Args, want) {
		t.Errorf("exit %d, stdout %q, stderr %q, ran %v; want 0, the report\n%s\nand both tasks handed to codex given %q", code, stdout, stderr, codex.commands, wantReport, want)
	}
}

func TestLoopRunWithPresetAndEffortFlagsRunsThePresetAtThatEffort(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	file := "presets:\n" +
		"  opus: {agent: claude, model: opus, effort: medium}\n" +
		"  deep: {agent: claude, model: sonnet, effort: high}\n" +
		"roles:\n" +
		"  work: opus\n"
	w, _ := repoWith(t, gh, claude, file)

	stdout, stderr, code := runLoopRun(t, w, "42", "--preset", "deep", "--effort", "low")

	want := []string{"--model", "sonnet", "--effort", "low"}
	if code != 0 || !strings.Contains(stdout, "effort: low (flag)\n") || len(claude.commands) != 1 || claude.commands[0].Name != "claude" || !containsRun(claude.commands[0].Args, want) {
		t.Errorf("exit %d, stdout %q, stderr %q, ran %v; want claude given %q", code, stdout, stderr, claude.commands, want)
	}
}

func TestLoopRunRefusesInvalidSettingsBeforeAnyAgentRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		repoFile string
		flags    []string
		want     []string
	}{
		"unknown key":            {"push: true\n\nmodle: opus\n", nil, []string{"modle", "line 3"}},
		"flat agent key":         {"push: true\nagent: claude\n", nil, []string{"line 2", "agent is set in a preset now", "roles: {work: opus}"}},
		"unknown agent":          {"presets:\n  g: {agent: gemini}\n", nil, []string{"line 2", `"gemini"`, "known agents are claude, codex, pi"}},
		"malformed name":         {"presets:\n  Fast: {agent: claude}\n", nil, []string{"line 2", `preset name "Fast"`}},
		"unknown role":           {"presets:\n  opus: {agent: claude}\nroles:\n  review: opus\n", nil, []string{"line 4", `smith has no role "review"`}},
		"undefined preset":       {"roles:\n  work: missing\n", nil, []string{"line 2", `role work names preset "missing"`}},
		"unknown agent flag":     {"", []string{"--agent", "gemini"}, []string{"--agent", `"gemini"`}},
		"effort off scale":       {"", []string{"--effort", "extreme"}, []string{`"extreme"`, "low, medium, high"}},
		"undefined preset flag":  {opusRepoFile, []string{"--preset", "missing"}, []string{`--preset names preset "missing"`}},
		"preset and agent flags": {opusRepoFile, []string{"--preset", "codex", "--agent", "claude"}, []string{"--preset", "--agent", "a preset already names its agent and model"}},
		"preset and model flags": {opusRepoFile, []string{"--preset", "codex", "--model", "sonnet"}, []string{"--preset", "--model", "a preset already names its agent and model"}},
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
			idle := &idleAgent{}
			w, _ := repoWith(t, gh, idle, tc.repoFile)

			_, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			named := true
			for _, want := range tc.want {
				named = named && strings.Contains(stderr, want)
			}
			if code == 0 || !named || idle.runs != 0 || len(gh.ran) != 0 {
				t.Errorf("exit %d, stderr %q, %d agent runs, gh ran %v; want non-zero naming %q, no agent and no gh", code, stderr, idle.runs, gh.ran, tc.want)
			}
		})
	}
}
