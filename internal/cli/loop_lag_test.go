package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/workbench"
)

// lagging sets up a bench as fakeBench does, whose work branch has lag behind its base.
func lagging(lag workbench.Lag) func(context.Context, string, workbench.Request) (loop.Bench, workbench.Lag, error) {
	return func(ctx context.Context, dir string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		bench, _, err := fakeBench(ctx, dir, req)
		return bench, lag, err
	}
}

// branchWarnings returns the lines of stderr led by smith: warning: that name a work branch, leaving out the label-write warnings fakeTracker causes.
func branchWarnings(stderr string) []string {
	var lines []string
	for line := range strings.Lines(stderr) {
		if strings.HasPrefix(line, "smith: warning:") && strings.Contains(line, "work branch") {
			lines = append(lines, strings.TrimSuffix(line, "\n"))
		}
	}
	return lines
}

func TestLoopRunWarnsBeforeAnyAgentRunsOfAWorkBranchBehindItsBase(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		commits int
		want    string
	}{
		"one":  {commits: 1, want: "smith: warning: work branch smith/42-the-loop is 1 commit behind origin/main: merge or rebase origin/main into it"},
		"many": {commits: 3, want: "smith: warning: work branch smith/42-the-loop is 3 commits behind origin/main: merge or rebase origin/main into it"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
			claude := &fakeAgent{gh: gh}

			_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: lagging(workbench.Lag{Base: "main", Commits: tc.commits})}, "42")

			first, _, _ := strings.Cut(stderr, "\n")
			if code != 0 || first != tc.want || !slices.Equal(branchWarnings(stderr), []string{tc.want}) || !slices.Equal(claude.handed, []string{"43"}) {
				t.Errorf("exit %d, handed %v, stderr %q; want 0, #43 handed, and %q first and alone", code, claude.handed, stderr, tc.want)
			}
		})
	}
}

func TestLoopRunDoesNotWarnOfAWorkBranchNotBehindItsBase(t *testing.T) {
	t.Parallel()
	for name, lag := range map[string]workbench.Lag{
		"level or ahead": {Base: "main"},
		"not compared":   {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}

			_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, bench: lagging(lag)}, "42")

			if code != 0 || len(branchWarnings(stderr)) != 0 {
				t.Errorf("exit %d, stderr %q; want 0 and no warning", code, stderr)
			}
		})
	}
}

func TestLoopRunWarnsThatTheBehindCheckCouldNotRunAndCarriesOn(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &fakeAgent{gh: gh}
	lag := workbench.Lag{Base: "main", Err: errors.New("fetch origin's main: could not read from remote")}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, bench: lagging(lag)}, "42")

	want := "smith: warning: could not check whether work branch smith/42-the-loop is behind origin/main: fetch origin's main: could not read from remote"
	first, _, _ := strings.Cut(stderr, "\n")
	if code != 0 || first != want || !slices.Equal(branchWarnings(stderr), []string{want}) || !slices.Equal(claude.handed, []string{"43"}) {
		t.Errorf("exit %d, handed %v, stderr %q; want 0, #43 handed, and %q first and alone", code, claude.handed, stderr, want)
	}
}
