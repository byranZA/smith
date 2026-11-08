package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/workbench"
)

// clonedRepo returns a real clone whose origin is a real bare repo with main
// as its default branch.
func clonedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote, clone := filepath.Join(dir, "remote.git"), filepath.Join(dir, "clone")
	gitIn(t, dir, "init", "--bare", "--initial-branch=main", remote)
	gitIn(t, dir, "clone", remote, clone)
	gitIn(t, clone, "checkout", "-B", "main")
	gitIn(t, clone, "commit", "--allow-empty", "-m", "seed")
	gitIn(t, clone, "push", "origin", "main")
	return clone
}

// planIn is the real workbench.Plan, run with the real git against clone.
func planIn(clone string) func(context.Context, string, workbench.Request) (string, string, error) {
	return func(ctx context.Context, _ string, req workbench.Request) (string, string, error) {
		return workbench.Plan(ctx, connection.System(), clone, req)
	}
}

func TestLoopListNamesTheDefaultWorkBranchAndBase(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": `{"number":42,"title":"Agent guide","state":"OPEN","body":"","labels":[],"blockedBy":{"nodes":[]}}`,
	}}

	stdout, stderr, code := runLoopList(t, gh, "42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "branch: smith/42-agent-guide\nbase:   main\n"; !strings.Contains(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to contain %q", stdout, want)
	}
}

func TestLoopListCreatesNoBranch(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": `{"number":42,"title":"Agent guide","state":"OPEN","body":"","labels":[],"blockedBy":{"nodes":[]}}`,
	}}
	clone := clonedRepo(t)
	refs := func() string { return gitIn(t, clone, "for-each-ref") + gitIn(t, clone, "worktree", "list") }
	before := refs()
	w := loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, plan: planIn(clone)}

	_, stderr, code := runLoopListIn(t, w, "42")

	if after := refs(); code != 0 || after != before {
		t.Errorf("exit code = %d, refs after =\n%s\nwant 0 and refs unchanged from\n%s(stderr: %s)", code, after, before, stderr)
	}
}

func TestLoopListNamesAnAdoptedWorkBranchOfTheSpecsNumber(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": `{"number":42,"title":"Agent guide","state":"OPEN","body":"","labels":[],"blockedBy":{"nodes":[]}}`,
	}}
	clone := clonedRepo(t)
	gitIn(t, clone, "branch", "smith/42-old-title")

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, plan: planIn(clone)}, "42")

	if want := "branch: smith/42-old-title\nbase:   main\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit code = %d, stdout =\n%s\nwant 0 and %q (stderr: %s)", code, stdout, want, stderr)
	}
}

func TestLoopListNamesTheBranchAndBaseTheFlagsGive(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": `{"number":42,"title":"Agent guide","state":"OPEN","body":"","labels":[],"blockedBy":{"nodes":[]}}`,
	}}
	cmd := newLoopCmd(inRepo(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}))
	cmd.SetArgs([]string{"list", "42", "--branch", "feature/x", "--base", "develop"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if want := "branch: feature/x\nbase:   develop\n"; !strings.Contains(out.String(), want) {
		t.Errorf("stdout =\n%s\nwant it to contain %q", out.String(), want)
	}
}

func TestLoopListShowsTheCapFromMaxIterations(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", "--max-iterations", "5")

	if code != 0 || !strings.Contains(stdout, "cap:    5 agent runs (--max-iterations)\n") {
		t.Errorf("exit %d, stdout %q, stderr %q; want the cap line 5 (flag)", code, stdout, stderr)
	}
}

func TestLoopListShowsNoCapFromMaxIterationsZero(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", "--max-iterations", "0")

	if code != 0 || !strings.Contains(stdout, "cap:    none (--max-iterations)\n") {
		t.Errorf("exit %d, stdout %q, stderr %q; want the cap line none (flag)", code, stdout, stderr)
	}
}

func TestLoopListShowsTheAttemptsFromMaxAttempts(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", "--max-attempts", "1")

	if code != 0 || !strings.Contains(stdout, "attempts: 1 (--max-attempts)\n") {
		t.Errorf("exit %d, stdout %q, stderr %q; want the attempts line 1 (flag)", code, stdout, stderr)
	}
}

func TestLoopListRefusesLimitsBelowTheirMinimum(t *testing.T) {
	for _, tc := range []struct{ flag, value string }{{"--max-iterations", "-1"}, {"--max-attempts", "0"}} {
		flag := tc.flag
		t.Run(flag, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{}}

			_, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", flag, tc.value)

			if code == 0 || !strings.Contains(stderr, flag) || len(gh.ran) != 0 {
				t.Errorf("exit %d, stderr %q, ran %v; want non-zero naming %s, gh never run", code, stderr, gh.ran, flag)
			}
		})
	}
}
