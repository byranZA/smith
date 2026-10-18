package session_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
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

// attachEnv stands a live session up, driven by command (none for a human),
// and answers with the env it lives in, the exec boundary attach would cross,
// and the session's name.
func attachEnv(t *testing.T, command []string) (session.Env, *fakeExec, string) {
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
	started, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Command: command})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	return env, execer, started.Name
}

func TestAttachConnectsWritableToAHumanDrivenSessionByDefault(t *testing.T) {
	env, execer, name := attachEnv(t, nil)
	var out strings.Builder

	if err := session.Attach(context.Background(), env, name, session.ByDriver, &out); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Errorf("exec argv = %v, want one %v", execer.calls, want)
	}
	if out.String() != "" {
		t.Errorf("Attach() wrote %q, want nothing for a writable attach", out.String())
	}
}

// TestAttachConnectsReadOnlyToALoopDrivenSessionByDefault pins the argv of a
// read-only attach: the session's status line gains a marker only a read-only
// client renders, prepended once to whatever status-left already is, and then
// the client attaches with -r.
func TestAttachConnectsReadOnlyToALoopDrivenSessionByDefault(t *testing.T) {
	env, execer, name := attachEnv(t, loopCommand)

	if err := session.Attach(context.Background(), env, name, session.ByDriver, io.Discard); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux",
		"set-option", "-o", "-q", "-F", "-t", "smith/smith-spec-42", "status-left", "##{?client_readonly,[read-only] ,}#{status-left}",
		";", "attach-session", "-r", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Errorf("exec argv = %v, want one %v", execer.calls, want)
	}
}

func TestAttachConnectsWritableToAWorktreeWithNoSessionRecord(t *testing.T) {
	execer := &fakeExec{}
	env := smithEnv(t, &tmuxServer{live: map[string]bool{"smith/smith-spec-42": true}})
	env.Exec = execer
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	git(t, bare, "worktree", "add", "-b", "spec-42", filepath.Join(env.Workspace, "smith", "worktrees", "spec-42"), "main")

	if err := session.Attach(context.Background(), env, "smith-spec-42", session.ByDriver, io.Discard); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Errorf("exec argv = %v, want one %v", execer.calls, want)
	}
}

// TestAttachInteractsWithALoopDrivenSessionOnRequest locks in the override:
// asking to interact connects writable to the session the driver would have
// had read-only, with no marker set and nothing said.
func TestAttachInteractsWithALoopDrivenSessionOnRequest(t *testing.T) {
	env, execer, name := attachEnv(t, loopCommand)
	var out strings.Builder

	if err := session.Attach(context.Background(), env, name, session.Interact, &out); err != nil {
		t.Fatalf("Attach() err = %v", err)
	}

	want := []string{"tmux", "attach-session", "-t", "smith/smith-spec-42"}
	if len(execer.calls) != 1 || !equal(execer.calls[0], want) {
		t.Errorf("exec argv = %v, want one %v", execer.calls, want)
	}
	if out.String() != "" {
		t.Errorf("Attach() wrote %q, want nothing for a writable attach", out.String())
	}
}

func TestAReadOnlyAttachSaysSoAndNamesTheWritableReattach(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  hint.Invocation
		want string
	}{
		{"relayed", relayed, "attached read-only: detach with C-b d and reattach with `smith session attach smith-dev smith-spec-42 --interact` to type\n"},
		{"typed on the box", hint.Invocation{}, "attached read-only: detach with C-b d and reattach with `smith session attach smith-spec-42 --interact` to type\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, execer, name := attachEnv(t, loopCommand)
			env.Hint = tt.inv
			var out strings.Builder

			if err := session.Attach(t.Context(), env, name, session.ByDriver, &out); err != nil {
				t.Fatalf("Attach() err = %v", err)
			}

			if out.String() != tt.want {
				t.Errorf("Attach(%s) wrote %q, want %q", name, out.String(), tt.want)
			}
			if len(execer.calls) != 1 {
				t.Errorf("exec argv = %v, want the terminal handed over once after the notice", execer.calls)
			}
		})
	}
}

// TestAttachRefusesAStoppedSessionAndNamesStart locks in the refusal an
// operator can act on: the session is on disk but nothing is running in it, so
// attach hands back the command that resumes it.
func TestAttachRefusesAStoppedSessionAndNamesStart(t *testing.T) {
	env, execer, name := attachEnv(t, nil)
	if err := session.Stop(context.Background(), env, name); err != nil {
		t.Fatalf("Stop() err = %v", err)
	}

	err := session.Attach(context.Background(), env, name, session.ByDriver, io.Discard)

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
	env, execer, _ := attachEnv(t, nil)

	err := session.Attach(context.Background(), env, "smith-ghost", session.ByDriver, io.Discard)

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
	env, _, name := attachEnv(t, nil)

	if err := session.Attach(context.Background(), env, name, session.Interact, io.Discard); err != nil {
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
