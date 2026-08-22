package cli

import (
	"strings"
	"testing"
)

// TestSessionStopReportsTheSessionItEnded is the smoke test for the verb: the
// name an operator pastes in reaches the stop and the command says what it
// ended. That the worktree survives is internal/session's to prove.
func TestSessionStopReportsTheSessionItEnded(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	standUp(t, resolve, tmux, "smith", "spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "stop", "smith-spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session it stopped", stdout)
	}
}

// TestSessionStopIsUnderTheRootCommand locks in the surface an operator on the
// box types.
func TestSessionStopIsUnderTheRootCommand(t *testing.T) {
	underRoot(t, "stop")
}
