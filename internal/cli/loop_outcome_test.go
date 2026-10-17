package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// loopSession starts the loop-driven session for spec #42 on a box whose workspace holds "smith", and returns its tmux server, its box and its worktree.
func loopSession(t *testing.T) (*tmuxBox, boxResolver, string) {
	t.Helper()
	tmux := &tmuxBox{}
	w, workspace := sessionLoop(t, readySpec(), tmux)
	if _, stderr, code := runLoopRun(t, w, "42", "--repo", "smith"); code != 0 {
		t.Fatalf("loop run --repo exit %d, stderr %q; want 0", code, stderr)
	}
	return tmux, resolvedBox(workspace, "smith"), filepath.Join(workspace, "smith", "worktrees", "smith-42-the-loop")
}

// listedRow returns the fields of the row session list prints for the session named.
func listedRow(t *testing.T, resolve boxResolver, tmux *tmuxBox, name string) []string {
	t.Helper()
	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "list")
	if code != 0 {
		t.Fatalf("session list exit %d, stderr %q; want 0", code, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == name {
			return fields
		}
	}
	t.Fatalf("session list = %q, want a row for %s", stdout, name)
	return nil
}

func TestALoopRunAsASessionsRootProcessRecordsItsOutcomeForSessionList(t *testing.T) {
	tmux, resolve, dir := loopSession(t)
	gh := readySpec()
	w := inRepo(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath, git: connection.System(), workdir: func() (string, error) { return dir, nil }})
	if _, stderr, code := runLoopRun(t, w, "42", "--branch=smith/42-the-loop", "--in-session"); code != 0 {
		t.Fatalf("loop run --in-session exit %d, stderr %q; want 0", code, stderr)
	}
	stopSession(t, resolve, tmux, "smith-smith-42-the-loop")

	row := listedRow(t, resolve, tmux, "smith-smith-42-the-loop")

	if got := strings.Join(row[1:], " "); got != "stopped - 0 loop complete" {
		t.Errorf("session list row = %q, want %q", got, "stopped - 0 loop complete")
	}
}
