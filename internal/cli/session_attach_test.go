package cli

import (
	"strings"
	"testing"
)

// TestSessionAttachObservesByDefault is the smoke test for the verb: attach
// with no flag hands the terminal to tmux read-only.
func TestSessionAttachObservesByDefault(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")
	connect := &fakeExec{}

	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, connect), "attach", "smith-spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got, want := connect.line(t), "tmux attach-session -r -t smith/smith-spec-42"; got != want {
		t.Errorf("exec argv = %q, want %q", got, want)
	}
}

// TestSessionAttachInteractsOnRequest locks in the flag that asks for the
// writable access level on the same tmux session.
func TestSessionAttachInteractsOnRequest(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")
	connect := &fakeExec{}

	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, connect), "attach", "smith-spec-42", "--interact")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got, want := connect.line(t), "tmux attach-session -t smith/smith-spec-42"; got != want {
		t.Errorf("exec argv = %q, want %q", got, want)
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
