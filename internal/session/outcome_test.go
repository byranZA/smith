package session_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/session"
)

// listed lists the sessions in env and returns the one named.
func listed(t *testing.T, env session.Env, name string) session.Session {
	t.Helper()
	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	for _, s := range got {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("List() = %+v, want a session named %q", got, name)
	return session.Session{}
}

// startLoop starts a loop-driven session on branch spec-42 of "smith" in env and returns its worktree.
func startLoop(t *testing.T, env session.Env) string {
	t.Helper()
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Command: loopCommand}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	return filepath.Join(env.Workspace, "smith", "worktrees", "spec-42")
}

// conclude records outcome in the session record of the worktree at dir, as the loop does when it stops.
func conclude(t *testing.T, dir, outcome string) {
	t.Helper()
	record, err := session.OpenRecord(context.Background(), connection.System(), dir)
	if err != nil {
		t.Fatalf("OpenRecord(%q) err = %v", dir, err)
	}
	if err := record.Conclude(outcome); err != nil {
		t.Fatalf("Conclude(%q) err = %v", outcome, err)
	}
}

func TestAStoppedLoopDrivenSessionHasItsRecordedOutcome(t *testing.T) {
	tmux := &tmuxServer{}
	env := smithEnv(t, tmux)
	conclude(t, startLoop(t, env), "waiting on human")
	tmux.kill(session.TmuxSession("smith-spec-42"))

	got := listed(t, env, "smith-spec-42")

	if got.Driver != session.Loop || got.Outcome != "waiting on human" {
		t.Errorf("listed session = %+v, want driver loop and outcome %q", got, "waiting on human")
	}
}

func TestALoopDrivenSessionStartedAgainForgetsTheEarlierOutcome(t *testing.T) {
	tmux := &tmuxServer{}
	env := smithEnv(t, tmux)
	conclude(t, startLoop(t, env), "cap reached")
	tmux.kill(session.TmuxSession("smith-spec-42"))
	startLoop(t, env)

	if got := listed(t, env, "smith-spec-42"); got.Outcome != "" {
		t.Errorf("outcome of a loop session started again = %q, want none recorded", got.Outcome)
	}
}

func TestARemovedSessionTakesItsOutcomeWithIt(t *testing.T) {
	tmux := &tmuxServer{}
	env := smithEnv(t, tmux)
	dir := startLoop(t, env)
	conclude(t, dir, "complete")
	tmux.kill(session.TmuxSession("smith-spec-42"))
	if _, err := session.Remove(context.Background(), env, []string{"smith-spec-42"}, false); err != nil {
		t.Fatalf("Remove() err = %v", err)
	}
	git(t, filepath.Join(env.Workspace, "smith", "repo.git"), "worktree", "add", dir, "spec-42")

	got := listed(t, env, "smith-spec-42")

	if got.Driver != session.Human || got.Outcome != "" {
		t.Errorf("session on a removed session's branch = %+v, want driver human and no outcome", got)
	}
}

func TestReadoutShowsEachSessionsOutcome(t *testing.T) {
	tests := []struct {
		name    string
		session session.Session
		want    string
	}{
		{"live loop", session.Session{Name: "a", Branch: "a", Live: true, Driver: session.Loop}, "-"},
		{"live loop with an outcome left over", session.Session{Name: "a", Branch: "a", Live: true, Driver: session.Loop, Outcome: "complete"}, "-"},
		{"live human", session.Session{Name: "a", Branch: "a", Live: true}, "-"},
		{"stopped human", session.Session{Name: "a", Branch: "a"}, "-"},
		{"stopped loop complete", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "complete"}, "complete"},
		{"stopped loop waiting on human", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "waiting on human"}, "waiting on human"},
		{"stopped loop cap reached", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "cap reached"}, "cap reached"},
		{"stopped loop attempts exhausted", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "attempts exhausted"}, "attempts exhausted"},
		{"stopped loop stopped", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "stopped"}, "stopped"},
		{"stopped loop failed", session.Session{Name: "a", Branch: "a", Driver: session.Loop, Outcome: "failed"}, "failed"},
		{"stopped loop with no outcome recorded", session.Session{Name: "a", Branch: "a", Driver: session.Loop}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := session.Readout([]session.Session{tt.session}, hint.Invocation{})
			lines := strings.Split(out, "\n")
			header := strings.Fields(lines[0])
			row := strings.Join(strings.Fields(lines[1])[5:], " ")

			if len(header) != 6 || header[5] != "OUTCOME" || row != tt.want {
				t.Errorf("Readout(%+v) = %q, want column OUTCOME reading %q", tt.session, out, tt.want)
			}
		})
	}
}
