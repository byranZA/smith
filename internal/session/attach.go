package session

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Mode is how the access level a terminal connects to a session at is chosen.
// Read-only and writable are two access levels on one tmux session, not two
// mechanisms.
type Mode int

const (
	// ByDriver connects at the access level the session's driver calls for:
	// writable to a human-driven session, read-only to a loop-driven one,
	// which is what an operator who is there to watch an agent work wants.
	// Read-only is advisory: see Attach.
	ByDriver Mode = iota
	// Interact connects the terminal writable whatever drives the session,
	// which is what an operator who is there to work wants.
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
// access level mode asks for. A read-only connection first writes a line to
// out naming its state and the command that reattaches writable, and marks
// the client read-only in tmux's status line for as long as it is attached.
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
func Attach(ctx context.Context, env Env, name string, mode Mode, out io.Writer) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nameless(env)
	}
	found, err := find(ctx, env, name)
	if err != nil {
		return err
	}
	if !isLive(ctx, env.Tmux, name) {
		resume := env.Hint.Command("session start", "--repo", found.repo.Name, "--branch", found.wt.branch)
		return fmt.Errorf("session %q is not running: resume it with `%s`", name, resume)
	}
	observe, err := readOnly(ctx, env, found, mode)
	if err != nil {
		return err
	}
	args := []string{"attach-session", "-t", TmuxSession(name)}
	if observe {
		args = readOnlyArgs(TmuxSession(name))
	}
	if env.Exec == nil {
		return fmt.Errorf("connect to session %q: smith was built with no way to exec into tmux", name)
	}
	if observe {
		reattach := env.Hint.Command("session attach", name, "--interact")
		if _, err := fmt.Fprintf(out, "attached read-only: detach with C-b d and reattach with `%s` to type\n", reattach); err != nil {
			return fmt.Errorf("write the read-only notice for %q: %w", name, err)
		}
	}
	if err := env.Exec.Exec("tmux", args); err != nil {
		return fmt.Errorf("connect to the tmux session for %q: %w", name, err)
	}
	return nil
}

// markedOption is the session option that records smith has prepended the
// read-only marker to the session's status-left.
const markedOption = "@smith-read-only-marked"

// readOnlyArgs returns the tmux argv that attaches to target read-only, first
// prepending a marker only read-only clients render to its status-left unless
// markedOption says that was done.
func readOnlyArgs(target string) []string {
	mark := fmt.Sprintf("set-option -F -t '%s' status-left '##{?client_readonly,[read-only] ,}#{status-left}' ; set-option -t '%s' %s 1", target, target, markedOption)
	return []string{
		"if-shell", "-F", "-t", target, "#{?" + markedOption + ",0,1}", mark,
		";", "attach-session", "-r", "-t", target,
	}
}

// readOnly reports whether the session found is to be connected to read-only:
// only when mode leaves it to the driver and the loop drives the session.
func readOnly(ctx context.Context, env Env, found placed, mode Mode) (bool, error) {
	if mode == Interact {
		return false, nil
	}
	driver, _, err := readRecord(ctx, env.Git, found.wt.path)
	if err != nil {
		return false, err
	}
	return driver == Loop, nil
}
