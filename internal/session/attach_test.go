package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// fakeExec stands in for the exec boundary, which a test may not cross: a real
// exec would replace the test process. It records the argv smith would have
// handed the kernel, which is what a test asserts on.
type fakeExec struct {
	calls [][]string
}

func (f *fakeExec) Exec(name string, args []string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// attachEnv stands a live session up and answers with the env it lives in, the
// exec boundary attach would cross, and the session's name.
func attachEnv(t *testing.T) (session.Env, *fakeExec, string) {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	execer := &fakeExec{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
		Exec:      execer,
	}
	started, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	return env, execer, started.Name
}

// TestAttachObservesByDefault locks in the access level attach picks when the
// operator asks for none: read-only, because they may be there to watch an
// agent work rather than to type into its session.
func TestAttachObservesByDefault(t *testing.T) {
	env, execer, name := attachEnv(t)

	if err := session.Attach(context.Background(), env, name, session.Observe); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-r", "-t", session.TmuxSession(name)}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Fatalf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestAttachInteractsOnRequest locks in the other access level on the same
// tmux session: asking to interact drops the read-only flag and nothing else.
func TestAttachInteractsOnRequest(t *testing.T) {
	env, execer, name := attachEnv(t)

	if err := session.Attach(context.Background(), env, name, session.Interact); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-t", session.TmuxSession(name)}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Fatalf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestAttachRefusesAStoppedSessionAndNamesStart locks in the refusal an
// operator can act on: the session is on disk but nothing is running in it, so
// attach hands back the command that resumes it.
func TestAttachRefusesAStoppedSessionAndNamesStart(t *testing.T) {
	env, execer, name := attachEnv(t)
	if err := session.Stop(context.Background(), env, name); err != nil {
		t.Fatalf("Stop() err = %v", err)
	}

	err := session.Attach(context.Background(), env, name, session.Observe)

	if err == nil {
		t.Fatalf("Attach() err = nil, want a refusal for a stopped session")
	}
	for _, want := range []string{name, "session start", "--repo smith", "--branch spec-42"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Attach() err = %q, want it to contain %q", err, want)
		}
	}
	if len(execer.calls) != 0 {
		t.Errorf("exec argv = %v, want nothing exec'd for a stopped session", execer.calls)
	}
}

// TestAttachRefusesAnUnknownName locks in that attach never guesses at a near
// match: a name no session holds is said plainly and nothing is connected to.
func TestAttachRefusesAnUnknownName(t *testing.T) {
	env, execer, _ := attachEnv(t)

	err := session.Attach(context.Background(), env, "smith-ghost", session.Observe)

	if err == nil {
		t.Fatalf("Attach() err = nil, want a refusal for an unknown name")
	}
	if !strings.Contains(err.Error(), "smith-ghost") {
		t.Errorf("Attach() err = %q, want it to name what was asked for", err)
	}
	if strings.Contains(err.Error(), "smith-spec-42") {
		t.Errorf("Attach() err = %q, want it not to guess at a near match", err)
	}
	if len(execer.calls) != 0 {
		t.Errorf("exec argv = %v, want nothing exec'd for an unknown name", execer.calls)
	}
}

// TestDisconnectingLeavesTheSessionLive locks in what makes a session worth
// having: the terminal comes and goes, and the processes in the session do
// not. Attaching and returning is the whole of a disconnect.
func TestDisconnectingLeavesTheSessionLive(t *testing.T) {
	env, _, name := attachEnv(t)

	if err := session.Attach(context.Background(), env, name, session.Interact); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(got) != 1 || !got[0].Live {
		t.Fatalf("List() = %+v, want the session still live after a disconnect", got)
	}
}

// equal reports whether two argvs are the same.
func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
