package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// refusingReader stands in for the terminal a prompt would read from, and
// fails the test the moment anything reads it: rm must never ask.
type refusingReader struct {
	t *testing.T
}

func (r *refusingReader) Read([]byte) (int, error) {
	r.t.Error("the command read stdin, want no confirmation prompt ever")
	return 0, io.EOF
}

// TestSessionRemoveReportsTheSessionItReclaimed is the smoke test for the
// verb: the name an operator pastes in reaches the removal and the command
// says what it reclaimed. What survives the removal is internal/session's to
// prove.
func TestSessionRemoveReportsTheSessionItReclaimed(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &stoppedTmux{}, &fakeExec{}), "rm", "smith-spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session it removed", stdout)
	}
}

// TestSessionRemoveRefusesADirtyWorktreeWithoutPrompting locks in the refusal
// exit path: the refusal goes to stderr rather than stdout, it names the
// session and the flag that overrides it, the exit code is non-zero, and
// nothing was ever asked of the terminal. What the refusal enumerates is
// internal/session's to prove.
func TestSessionRemoveRefusesADirtyWorktreeWithoutPrompting(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	standUp(t, resolve, &fakeTmux{}, "smith", "spec-42")
	dirtyWorktree(t, workspace, "smith", "spec-42")
	cmd := newSessionCmd(onBoxWiring(t, resolve, &stoppedTmux{}, &fakeExec{}))
	cmd.SetArgs([]string{"rm", "smith-spec-42"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(&refusingReader{t: t})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	code := codeFromError(cmd.Execute())

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a dirty worktree")
	}
	for _, want := range []string{"smith-spec-42", "--force"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to carry %q", errOut.String(), want)
		}
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want a refusal to say nothing on stdout", out.String())
	}
}

// TestSessionRemoveForceOverridesTheRefusal locks in that one flag carries the
// override through the command: the removal the gate refused a moment ago now
// exits zero and says what it reclaimed.
func TestSessionRemoveForceOverridesTheRefusal(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	standUp(t, resolve, tmux, "smith", "spec-42")
	dirtyWorktree(t, workspace, "smith", "spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "rm", "smith-spec-42", "--force")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 under --force (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session it removed", stdout)
	}
}

// TestSessionRemoveTakesNoFiltersOfItsOwn locks in that filters live on the
// listing alone: a filter here would make the target set of a delete implicit,
// and a delete has no undo.
func TestSessionRemoveTakesNoFiltersOfItsOwn(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	standUp(t, resolve, tmux, "smith", "spec-42")
	stopSession(t, resolve, tmux, "smith-spec-42")

	_, _, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "rm", "--stopped")

	if code == 0 {
		t.Fatal("exit code = 0, want --stopped rejected as an unknown flag")
	}
	if got, want := listedNames(t, resolve, tmux), "smith-spec-42\n"; got != want {
		t.Errorf("names after the rejected flag = %q, want %q still there", got, want)
	}
}

// TestSessionListNamesComposeIntoABatchRemoval drives the one pipeline the
// names output exists for: the bare names one command printed are the argument
// vector the next one takes, and the sessions the filter left out are
// untouched.
func TestSessionListNamesComposeIntoABatchRemoval(t *testing.T) {
	workspace := t.TempDir()
	resolve, tmux := twoReposOneLive(t, workspace)

	listed, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "list", "--stopped", "--names")
	if code != 0 {
		t.Fatalf("session list exited %d (stderr: %s)", code, stderr)
	}

	_, stderr, code = runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}),
		append([]string{"rm"}, strings.Fields(listed)...)...)

	if code != 0 {
		t.Fatalf("session rm exited %d (stderr: %s)", code, stderr)
	}
	if got, want := listedNames(t, resolve, tmux), "smith-live-one\n"; got != want {
		t.Errorf("names after the batch = %q, want only the live session %q", got, want)
	}
}

// TestSessionRemoveIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionRemoveIsUnderTheRootCommand(t *testing.T) {
	underRoot(t, "rm")
}
