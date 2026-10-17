package session_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/session"
)

// loopCommand is the root command a loop-driven session is started with.
var loopCommand = []string{"/usr/local/bin/smith", "loop", "run", "42"}

// driverOf lists the sessions in env and returns the driver of the one named.
func driverOf(t *testing.T, env session.Env, name string) session.Driver {
	t.Helper()
	got, err := session.List(t.Context(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	for _, s := range got {
		if s.Name == name {
			return s.Driver
		}
	}
	t.Fatalf("List() = %+v, want a session named %q", got, name)
	return session.Human
}

// smithEnv is an Env over a workspace holding one bare repo "smith".
func smithEnv(t *testing.T, tmux *tmuxServer) session.Env {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
}

func TestASessionStartedWithACommandIsLoopDriven(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Command: loopCommand}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := driverOf(t, env, "smith-spec-42"); got != session.Loop {
		t.Errorf("driver of a session started with a command = %v, want %v", got, session.Loop)
	}
}

func TestASessionStartedWithoutACommandIsHumanDriven(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got := driverOf(t, env, "smith-spec-42"); got != session.Human {
		t.Errorf("driver of a session started without a command = %v, want %v", got, session.Human)
	}
}

func TestAStoppedLoopDrivenSessionKeepsItsDriver(t *testing.T) {
	tmux := &tmuxServer{}
	env := smithEnv(t, tmux)
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Command: loopCommand}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))

	if got := driverOf(t, env, "smith-spec-42"); got != session.Loop {
		t.Errorf("driver of a stopped loop-driven session = %v, want %v", got, session.Loop)
	}
}

func TestAHumanSessionOnALoopsWorktreeIsHumanDriven(t *testing.T) {
	tmux := &tmuxServer{}
	env := smithEnv(t, tmux)
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42", Command: loopCommand}); err != nil {
		t.Fatalf("Start() loop err = %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() human err = %v", err)
	}

	if got := driverOf(t, env, "smith-spec-42"); got != session.Human {
		t.Errorf("driver after a human took over a loop's worktree = %v, want %v", got, session.Human)
	}
}

func TestAWorktreeWithNoSessionRecordIsHumanDriven(t *testing.T) {
	env := smithEnv(t, &tmuxServer{})
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	dir := filepath.Join(env.Workspace, "smith", "worktrees", "spec-42")
	git(t, bare, "worktree", "add", "-b", "spec-42", dir, "main")

	if got := driverOf(t, env, "smith-spec-42"); got != session.Human {
		t.Errorf("driver of a worktree with no session record = %v, want %v", got, session.Human)
	}
}

func TestReadoutShowsEachSessionsDriver(t *testing.T) {
	tests := []struct {
		name    string
		session session.Session
		want    string
	}{
		{"live loop", session.Session{Name: "a", Branch: "a", Live: true, Driver: session.Loop}, "loop"},
		{"stopped loop", session.Session{Name: "a", Branch: "a", Driver: session.Loop}, "loop"},
		{"live human", session.Session{Name: "a", Branch: "a", Live: true, Driver: session.Human}, "human"},
		{"stopped human", session.Session{Name: "a", Branch: "a", Driver: session.Human}, "human"},
		{"no driver recorded", session.Session{Name: "a", Branch: "a"}, "human"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := session.Readout([]session.Session{tt.session}, hint.Invocation{})
			lines := strings.Split(out, "\n")
			header, row := strings.Fields(lines[0]), strings.Fields(lines[1])

			if len(row) != len(header) || header[4] != "DRIVER" || row[4] != tt.want {
				t.Errorf("Readout(%+v) = %q, want column DRIVER reading %q", tt.session, out, tt.want)
			}
		})
	}
}
