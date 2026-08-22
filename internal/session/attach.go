package session

import (
	"context"
	"fmt"
	"strings"
)

// Mode is the access level a terminal connects to a session at. Observe and
// Interact are two access levels on one tmux session, not two mechanisms.
type Mode int

const (
	// Observe connects the terminal read-only, which is what an operator who
	// is there to watch an agent work wants. It is advisory: see Attach.
	Observe Mode = iota
	// Interact connects the terminal writable, which is what an operator who
	// is there to work wants.
	Interact
)

// Execer replaces smith's own process with another command. It is the system
// boundary attach crosses — a real exec never returns, so it is injected and a
// test asserts on the argv smith would have handed the kernel.
type Execer interface {
	// Exec replaces the calling process with name run with args. It returns
	// only when the replacement did not happen.
	Exec(name string, args []string) error
}

// Attach connects the operator's terminal to a session's tmux session, at the
// access level mode asks for.
//
// smith execs into tmux rather than proxying it: the terminal then talks to
// tmux directly, so resizes and signals are tmux's to handle and there is no
// smith process in the middle copying bytes. Nothing about the session
// changes, which is why disconnecting leaves it running — the terminal came
// and went, and the processes in the session did not.
//
// It refuses on the two things it cannot connect to: a name no session holds,
// said plainly rather than guessed at a near match, and a session whose tmux
// session is not running, which names the start command that resumes it.
func Attach(ctx context.Context, env Env, name string, mode Mode) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("no session named: want the name `session list` reports")
	}
	found, err := find(ctx, env, name)
	if err != nil {
		return err
	}
	if !isLive(ctx, env.Tmux, name) {
		return fmt.Errorf("session %q is not running: resume it with `smith session start --repo %s --branch %s`", name, found.repo.Name, found.wt.branch)
	}
	args := []string{"attach-session", "-t", TmuxSession(name)}
	if mode == Observe {
		// tmux's -r is advisory, not a permission boundary (ADR-0005).
		// Anyone who can reach this box as the smith user can run
		// `tmux attach` themselves without it. What it buys is a guard
		// against typing into an agent's session by reflex, and that is all it
		// is ever to be relied on for.
		args = []string{"attach-session", "-r", "-t", TmuxSession(name)}
	}
	if env.Exec == nil {
		return fmt.Errorf("connect to session %q: smith was built with no way to exec into tmux", name)
	}
	if err := env.Exec.Exec("tmux", args); err != nil {
		return fmt.Errorf("connect to the tmux session for %q: %w", name, err)
	}
	return nil
}
