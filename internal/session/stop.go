package session

import (
	"context"
	"fmt"
	"strings"
)

// Stop ends a session's tmux session and keeps its worktree and its branch.
//
// It gets no safety gate, deliberately: what it ends is a process, and every
// byte the operator has on disk survives it. A gate here would train the
// operator to answer one at the verb that has nothing to lose, which is the
// habit that makes them answer the one on rm reflexively too.
//
// It is idempotent. Running state is probed rather than remembered, so a
// session that has already ended — stopped by hand, or its root process exited
// from inside — is already in the state Stop asks for, and Stop says so by
// succeeding and touching nothing.
func Stop(ctx context.Context, env Env, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("no session named: want the name `session list` reports")
	}
	if !isLive(ctx, env.Tmux, name) {
		return nil
	}
	if _, err := run(ctx, env.Tmux, "tmux", "kill-session", "-t", TmuxSession(name)); err != nil {
		return fmt.Errorf("end the tmux session for %q: %w", name, err)
	}
	return nil
}
