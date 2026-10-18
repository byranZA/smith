package cli

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// fakePreparer records each worktree it prepares and fails each with err when set.
type fakePreparer struct {
	prepared []string
	err      error
}

func (f *fakePreparer) Prepare(_ context.Context, dir string, _ io.Writer) error {
	f.prepared = append(f.prepared, dir)
	return f.err
}

func TestLoopRunPreparesEachTaskWorktree(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": taskJSON(44, "OPEN", "ready-for-agent"),
	}}
	preparer := &fakePreparer{}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, preparer: preparer}, "42")

	if want := []string{"/bench/43", "/bench/44"}; code != 0 || !slices.Equal(preparer.prepared, want) {
		t.Errorf("exit %d, prepared %v (stderr %q); want 0 and %v", code, preparer.prepared, stderr, want)
	}
}

func TestLoopRunWhosePreparationFailsRunsNoAgentAndNamesTheWorktree(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	claude := &fakeAgent{gh: gh}
	preparer := &fakePreparer{err: errors.New("the prepare step exited with status 2: npm ci")}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath, preparer: preparer}, "42")

	if code == 0 || len(claude.handed) != 0 || !strings.Contains(stderr, "prepare the worktree of #43 in /bench/43: the prepare step exited with status 2") {
		t.Errorf("exit %d, handed %v, stderr %q; want non-zero, no agent run and the failed preparation named", code, claude.handed, stderr)
	}
}
