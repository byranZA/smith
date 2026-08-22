package cli

import (
	"strings"
	"testing"
)

// TestSessionAttachHandsTheTerminalOver is the smoke test for the verb: the
// name an operator pastes in reaches the attach, and the terminal is handed to
// one command rather than streamed through smith. Which command that is, and
// at which access level, is internal/session's to prove.
func TestSessionAttachHandsTheTerminalOver(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")
	connect := &fakeExec{}

	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, connect), "attach", "smith-spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(connect.calls) != 1 {
		t.Errorf("exec called %d times, want the terminal handed over once: %v", len(connect.calls), connect.calls)
	}
}

// TestSessionAttachInteractChangesTheConnection locks in the one thing the
// flag is for: --interact reaches the access level the connection is made at,
// so the same session is connected to differently than it is by default.
func TestSessionAttachInteractChangesTheConnection(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")
	observe, interact := &fakeExec{}, &fakeExec{}

	mustAttach(t, resolve, observe, "smith-spec-42")
	mustAttach(t, resolve, interact, "smith-spec-42", "--interact")

	if observe.line(t) == interact.line(t) {
		t.Errorf("--interact connected as %q, want it to differ from the default connection", interact.line(t))
	}
}

// TestSessionAttachRefusesAnUnknownName locks in the refusal exit path a
// session refusal takes through the command: the report lands on stderr, the
// exit code is non-zero, and the terminal is handed to nothing.
func TestSessionAttachRefusesAnUnknownName(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	connect := &fakeExec{}

	_, stderr, code := runSessionOn(t,
		onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, connect),
		"attach", "smith-ghost")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an unknown session name")
	}
	if !strings.Contains(stderr, "smith-ghost") {
		t.Errorf("stderr = %q, want it to name what was asked for", stderr)
	}
	if len(connect.calls) != 0 {
		t.Errorf("exec argv = %v, want nothing connected to", connect.calls)
	}
}

// TestSessionAttachIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionAttachIsUnderTheRootCommand(t *testing.T) {
	underRoot(t, "attach")
}
