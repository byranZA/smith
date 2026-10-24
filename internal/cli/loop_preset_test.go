package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/workbench"
)

// presetTask is task n, open or closed by state, ready for an agent and
// carrying a preset: label for each of presets.
func presetTask(n int, state string, presets ...string) string {
	labels := []string{`{"name":"ready-for-agent"}`}
	for _, p := range presets {
		labels = append(labels, fmt.Sprintf(`{"name":"preset:%s"}`, p))
	}
	return fmt.Sprintf(`{"number":%d,"title":"Task %d","state":%q,"body":"","labels":[%s],"blockedBy":{"nodes":[]}}`, n, n, state, strings.Join(labels, ","))
}

// countingBench counts the work branches w sets up.
func countingBench(w loopWiring) (loopWiring, *int) {
	var benches int
	w.bench = func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		benches++
		return fakeBench(ctx, dir, req)
	}
	return w, &benches
}

func TestLoopRunRefusesATasksPresetLabelsBeforeSettingUpTheWorkBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		presets []string
		flags   []string
		want    string
	}{
		"undefined preset":              {[]string{"missing"}, nil, `#45's label preset:missing names preset "missing"`},
		"two presets":                   {[]string{"opus", "codex"}, nil, "#45 carries the labels preset:opus and preset:codex"},
		"undefined preset, interactive": {[]string{"missing"}, []string{"--interactive"}, `#45's label preset:missing names preset "missing"`},
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{
				"42": specJSON(43, 44, 45),
				"43": taskJSON(43, "OPEN", "ready-for-agent"),
				"44": taskJSON(44, "OPEN", "ready-for-agent"),
				"45": presetTask(45, "OPEN", tc.presets...),
			}}
			idle := &idleAgent{}
			w, _ := repoWith(t, gh, idle, opusRepoFile)
			w, benches := countingBench(w)

			_, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			if code == 0 || !strings.Contains(stderr, tc.want) || *benches != 0 || idle.runs != 0 {
				t.Errorf("exit %d, stderr %q, %d work branches, %d agent runs; want non-zero naming %q, no work branch and no agent", code, stderr, *benches, idle.runs, tc.want)
			}
		})
	}
}

func TestLoopRunPinnedByAFlagDoesNotCheckTaskPresetLabels(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": presetTask(43, "OPEN", "missing")}}
	codex := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, codex, opusRepoFile)

	_, stderr, code := runLoopRun(t, w, "42", "--preset", "codex")

	if code != 0 || len(codex.commands) != 1 || codex.commands[0].Name != "codex" {
		t.Errorf("exit %d, stderr %q, ran %v; want 0 and #43 handed to codex", code, stderr, codex.commands)
	}
}

func TestLoopRunDoesNotCheckAClosedTasksPresetLabel(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": presetTask(43, "CLOSED", "missing"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, opusRepoFile)

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(claude.commands) != 1 || claude.commands[0].Name != "claude" {
		t.Errorf("exit %d, stderr %q, ran %v; want 0 and #44 handed to claude", code, stderr, claude.commands)
	}
}

func TestLoopRunOnABoxRefusesATasksPresetLabelsBeforeStartingASession(t *testing.T) {
	tmux := &tmuxBox{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": presetTask(43, "OPEN", "missing")}}
	w, workspace := sessionLoop(t, gh, tmux)

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	want := `#43's label preset:missing names preset "missing"`
	_, statErr := os.Stat(filepath.Join(workspace, "smith", "worktrees"))
	if code == 0 || !strings.Contains(stderr, want) || len(tmux.calls) != 0 || !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("exit %d, stderr %q, tmux %q, worktrees %v; want non-zero naming %q, no session and no worktree", code, stderr, tmux.calls, statErr, want)
	}
}

func TestLoopRunOnABoxChecksTaskPresetsAgainstTheBaseTheBranchIsCutFrom(t *testing.T) {
	tmux := &tmuxBox{}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": presetTask(43, "OPEN", "deep")}}
	w, workspace := sessionLoop(t, gh, tmux)
	upstream := gitStdout(t, filepath.Join(workspace, "smith", "repo.git"), "remote", "get-url", "origin")
	writeRepoFile(t, upstream, "repo.yaml", "presets:\n  deep: {agent: claude, effort: high}\n")
	runGit(t, upstream, "add", ".smith")
	runGit(t, upstream, "commit", "-m", "repo file")

	_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

	if code != 0 || len(started(tmux)) != 1 {
		t.Errorf("exit %d, stderr %q, sessions %q; want 0 and a session started", code, stderr, started(tmux))
	}
}

// installedOnly is a lookPath that finds only the programs named.
func installedOnly(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if !slices.Contains(names, name) {
			return "", fmt.Errorf("%s: executable file not found in $PATH", name)
		}
		return "/usr/local/bin/" + name, nil
	}
}

func TestLoopRunChecksOnlyTheAgentsItsTasksPresetsUse(t *testing.T) {
	for name, tc := range map[string]struct {
		flags     []string
		installed []string
		want      []string
	}{
		"a labelled task without the unused work agent": {nil, []string{"codex"}, []string{"codex"}},
		"interactive, the selected task's agent alone":  {[]string{"--interactive"}, []string{"codex"}, []string{"codex"}},
		"pinned, the pinned agent alone":                {[]string{"--agent", "claude"}, []string{"claude"}, []string{"claude"}},
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": presetTask(43, "OPEN", "codex")}}
			launched := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
			w, _ := repoWith(t, gh, launched, opusRepoFile)
			w.lookPath = installedOnly(tc.installed...)

			_, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			var got []string
			for _, cmd := range launched.commands {
				got = append(got, cmd.Name)
			}
			if code != 0 || !slices.Equal(got, tc.want) {
				t.Errorf("exit %d, stderr %q, launched %v; want 0 and %v", code, stderr, got, tc.want)
			}
		})
	}
}

func TestLoopRunRefusesATasksMissingAgentBeforeSettingUpTheWorkBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		task      string
		flags     []string
		installed string
		want      string
	}{
		"a labelled task's agent":           {presetTask(44, "OPEN", "codex"), nil, "claude", "agent codex is not installed"},
		"the selected task's agent":         {presetTask(44, "OPEN", "codex"), []string{"--interactive"}, "codex", "agent claude is not installed"},
		"the pinned agent over every label": {presetTask(44, "OPEN", "opus"), []string{"--preset", "codex"}, "claude", "agent codex is not installed"},
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": tc.task}}
			idle := &idleAgent{}
			w, _ := repoWith(t, gh, idle, opusRepoFile)
			w.lookPath = installedOnly(tc.installed)
			w, benches := countingBench(w)

			_, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			if code == 0 || !strings.Contains(stderr, tc.want) || *benches != 0 || idle.runs != 0 {
				t.Errorf("exit %d, stderr %q, %d work branches, %d agent runs; want non-zero naming %q, no work branch and no agent", code, stderr, *benches, idle.runs, tc.want)
			}
		})
	}
}

func TestLoopRunOnABoxChecksTheAgentsItsTasksPresetsUse(t *testing.T) {
	for name, tc := range map[string]struct {
		installed []string
		want      string
	}{
		"a labelled task without the unused work agent starts": {[]string{"codex"}, ""},
		"a labelled task's missing agent is refused":           {[]string{"claude"}, "agent codex is not installed"},
	} {
		t.Run(name, func(t *testing.T) {
			tmux := &tmuxBox{}
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": presetTask(43, "OPEN", "codex")}}
			w, workspace := sessionLoop(t, gh, tmux)
			upstream := gitStdout(t, filepath.Join(workspace, "smith", "repo.git"), "remote", "get-url", "origin")
			writeRepoFile(t, upstream, "repo.yaml", opusRepoFile)
			runGit(t, upstream, "add", ".smith")
			runGit(t, upstream, "commit", "-m", "repo file")
			w.mise = &boxMise{installed: tc.installed}

			_, stderr, code := runLoopRun(t, w, "42", "--repo", "smith")

			if tc.want == "" && (code != 0 || len(started(tmux)) != 1) {
				t.Errorf("exit %d, stderr %q, started %q; want 0 and one session", code, stderr, started(tmux))
			}
			if tc.want != "" && (code == 0 || !strings.Contains(stderr, tc.want) || len(started(tmux)) != 0) {
				t.Errorf("exit %d, stderr %q, started %q; want non-zero naming %q and no session", code, stderr, started(tmux), tc.want)
			}
		})
	}
}
