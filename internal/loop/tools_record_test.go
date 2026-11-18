package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/smithtool"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/tracker"
)

func TestASuccessfulStackUpCallShowsOnTheTerminalWithItsDuration(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.Launcher = &callingLauncher{next: l.Launcher, name: "stack-up"}
	l.ToolsDir = toolsDir(t)
	var progress strings.Builder
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(progress.String(), "smith tool: stack-up ok (0s)\n") {
		t.Errorf("progress = %q, want the line %q", progress.String(), "smith tool: stack-up ok (0s)")
	}
}

func TestAFailedStackUpCallShowsItsExitStatusAndThenUpsOutputTail(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.Stack = &stack.Stack{Command: &failSecondUp{stackCommands: stackCommands{events: &j.events}}}
	l.Launcher = &callingLauncher{next: l.Launcher, name: "stack-up"}
	l.ToolsDir = toolsDir(t)
	var progress strings.Builder
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	line, tail := "smith tool: stack-up failed (exit 7, 0s)\npulling images\n", progress.String()
	if !strings.Contains(tail, line) {
		t.Errorf("progress = %q, want it to contain %q", tail, line)
	}
}

// reupsPerAttempt wraps a launcher and, for launch i, calls stack-up counts[i] times before the agent runs.
type reupsPerAttempt struct {
	next   loop.Launcher
	counts []int
	launch int
}

func (r *reupsPerAttempt) Launch(ctx context.Context, cmd agent.Command) error {
	var callErrs error
	for _, kv := range cmd.Env {
		if path, ok := strings.CutPrefix(kv, loop.ToolsEnv+"="); ok {
			for range r.counts[r.launch] {
				// A re-up that ran and failed is what some tests set up; any other error is not.
				var failed *smithtool.FailedError
				if err := smithtool.Call(ctx, path, "stack-up", nil, io.Discard); err != nil && !errors.As(err, &failed) {
					callErrs = errors.Join(callErrs, err)
				}
			}
		}
	}
	r.launch++
	return errors.Join(callErrs, r.next.Launch(ctx, cmd))
}

// reupReport runs two attempts of task 43, the agent re-upping counts[i] times in attempt i, and returns the posted report headers.
func reupReport(t *testing.T, commands stack.Commander, counts ...int) []string {
	t.Helper()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	r := newReporter(t, b, "## Stuck\n")
	l := reportingLoop(t, r)
	l.Bench = stackedReporter{reporter: r, dir: repoFileIn(t, t.TempDir())}
	l.Stack = &stack.Stack{Command: commands}
	l.Launcher = &reupsPerAttempt{next: r, counts: counts}
	l.ToolsDir = toolsDir(t)
	l.Limits.MaxAttempts = len(counts)
	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var headers []string
	for _, c := range commentsOn(b, 43) {
		headers = append(headers, strings.SplitN(c, "\n", 2)[0])
	}
	return headers
}

func TestTheAttemptReportHeaderCountsTheAttemptsReupsNotTheRuns(t *testing.T) {
	t.Parallel()
	j := &journal{}
	got := reupReport(t, stackCommands{events: &j.events}, 2, 0)

	want := []string{
		"smith: attempt 1 of 2, left open, on preset default, took 1m30s, re-upped the stack 2 times.",
		"smith: attempt 2 of 2, left open, on preset default, took 1m30s.",
	}
	if !slices.Equal(got[:2], want) {
		t.Errorf("headers = %q, want %q", got[:2], want)
	}
}

func TestTheAttemptReportHeaderSaysWhenTheLastReupFailed(t *testing.T) {
	t.Parallel()
	j := &journal{}
	got := reupReport(t, &failSecondUp{stackCommands: stackCommands{events: &j.events}}, 1)

	want := "smith: attempt 1 of 1, left open, on preset default, took 1m30s, re-upped the stack 1 time, the last failed."
	if got[0] != want {
		t.Errorf("header = %q, want %q", got[0], want)
	}
}

// stackedReporter is a reporter whose every task worktree is the real directory dir.
type stackedReporter struct {
	*reporter
	dir string
}

func (s stackedReporter) Open(context.Context, int) (string, error) { return s.dir, nil }

func TestAnUnknownToolCallShowsAsRefusedOnTheTerminal(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.Launcher = &callingLauncher{next: l.Launcher, name: "frobnicate"}
	l.ToolsDir = toolsDir(t)
	var progress strings.Builder
	l.Progress = &progress

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := `smith tool: frobnicate refused (unknown smith tool "frobnicate"; available in this run: stack-up)` + "\n"
	if !strings.Contains(progress.String(), want) {
		t.Errorf("progress = %q, want it to contain %q", progress.String(), want)
	}
}
