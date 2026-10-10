package session_test

import (
	"context"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/session"
)

// relayed is a command the operator relayed to the box they call smith-dev.
var relayed = hint.Invocation{Box: "smith-dev"}

// hintedEnv stands a session up on smith's spec-42 branch, in an env whose
// suggested commands are spelled for inv, and answers with the env and the
// tmux server the session runs on.
func hintedEnv(t *testing.T, inv hint.Invocation) (session.Env, *tmuxServer) {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
		Exec:      &fakeExec{},
		Hint:      inv,
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	return env, tmux
}

func TestAttachToAStoppedSessionNamesTheStartThatResumesIt(t *testing.T) {
	tests := []struct {
		name string
		inv  hint.Invocation
		want string
	}{
		{"relayed", relayed, "session \"smith-spec-42\" is not running: resume it with `smith session start smith-dev --repo smith --branch spec-42`"},
		{"relayed to a literal target", hint.Invocation{Box: "smith@10.0.0.4"}, "session \"smith-spec-42\" is not running: resume it with `smith session start smith@10.0.0.4 --repo smith --branch spec-42`"},
		{"typed on the box", hint.Invocation{}, "session \"smith-spec-42\" is not running: resume it with `smith session start --repo smith --branch spec-42`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := hintedEnv(t, tt.inv)
			if err := session.Stop(context.Background(), env, "smith-spec-42"); err != nil {
				t.Fatalf("Stop() err = %v", err)
			}
			err := session.Attach(context.Background(), env, "smith-spec-42", session.Observe)
			if err == nil || err.Error() != tt.want {
				t.Errorf("Attach(smith-spec-42) err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestANamelessSessionVerbNamesTheListing(t *testing.T) {
	tests := []struct {
		name string
		inv  hint.Invocation
		verb func(session.Env) error
		want string
	}{
		{"attach, relayed", relayed, func(env session.Env) error {
			return session.Attach(context.Background(), env, " ", session.Observe)
		}, "no session named: want the name `smith session list smith-dev` reports"},
		{"attach, typed on the box", hint.Invocation{}, func(env session.Env) error {
			return session.Attach(context.Background(), env, " ", session.Observe)
		}, "no session named: want the name `smith session list` reports"},
		{"stop, relayed", relayed, func(env session.Env) error {
			return session.Stop(context.Background(), env, "")
		}, "no session named: want the name `smith session list smith-dev` reports"},
		{"stop, typed on the box", hint.Invocation{}, func(env session.Env) error {
			return session.Stop(context.Background(), env, "")
		}, "no session named: want the name `smith session list` reports"},
		{"rm, relayed", relayed, func(env session.Env) error {
			_, err := session.Remove(context.Background(), env, []string{" "}, false)
			return err
		}, "no session named: want the names `smith session list smith-dev` reports"},
		{"rm, typed on the box", hint.Invocation{}, func(env session.Env) error {
			_, err := session.Remove(context.Background(), env, []string{" "}, false)
			return err
		}, "no session named: want the names `smith session list` reports"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := session.Env{Hint: tt.inv}
			if err := tt.verb(env); err == nil || err.Error() != tt.want {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAnUnknownSessionNamesTheListing(t *testing.T) {
	tests := []struct {
		name string
		inv  hint.Invocation
		want string
	}{
		{"relayed", relayed, "no session named \"smith-spec-43\" exists on this box: `smith session list smith-dev` reports the sessions there are"},
		{"typed on the box", hint.Invocation{}, "no session named \"smith-spec-43\" exists on this box: `smith session list` reports the sessions there are"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, _ := hintedEnv(t, tt.inv)
			err := session.Stop(context.Background(), env, "smith-spec-43")
			if err == nil || err.Error() != tt.want {
				t.Errorf("Stop(smith-spec-43) err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRemovalRefusalsNameTheCommandsThatClearThem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *session.RemovalRefusedError
		want string
	}{
		{
			"live, relayed",
			&session.RemovalRefusedError{Name: "smith-spec-42", Live: true, Hint: relayed},
			"session `smith/smith-spec-42` is running — `smith session stop smith-dev smith-spec-42` first",
		},
		{
			"live, typed on the box",
			&session.RemovalRefusedError{Name: "smith-spec-42", Live: true},
			"session `smith/smith-spec-42` is running — `smith session stop smith-spec-42` first",
		},
		{
			"dirty, relayed",
			&session.RemovalRefusedError{Name: "smith-spec-42", Modified: 1, Hint: relayed},
			"worktree of session \"smith-spec-42\" holds uncommitted work: 1 modified, 0 staged, 0 untracked; reclaim it and lose that work with `smith session rm smith-dev smith-spec-42 --force`",
		},
		{
			"dirty, typed on the box",
			&session.RemovalRefusedError{Name: "smith-spec-42", Modified: 1},
			"worktree of session \"smith-spec-42\" holds uncommitted work: 1 modified, 0 staged, 0 untracked; reclaim it and lose that work with `smith session rm smith-spec-42 --force`",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("%+v.Error() = %q, want %q", *tt.err, got, tt.want)
			}
		})
	}
}

func TestRemoveRelayedRefusesALiveSessionNamingTheBox(t *testing.T) {
	env, _ := hintedEnv(t, relayed)

	_, err := session.Remove(context.Background(), env, []string{"smith-spec-42"}, false)

	want := "session `smith/smith-spec-42` is running — `smith session stop smith-dev smith-spec-42` first"
	if err == nil || err.Error() != want {
		t.Errorf("Remove(smith-spec-42) err = %v, want %q", err, want)
	}
}

func TestAnEmptyReadoutNamesTheStartCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  hint.Invocation
		want string
	}{
		{"relayed", relayed, "no sessions\n\nStart one with:\n  smith session start smith-dev --repo <name> --branch <name>\n"},
		{"typed on the box", hint.Invocation{}, "no sessions\n\nStart one with:\n  smith session start --repo <name> --branch <name>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := session.Readout(nil, tt.inv); got != tt.want {
				t.Errorf("Readout(nil, %+v) = %q, want %q", tt.inv, got, tt.want)
			}
		})
	}
}
