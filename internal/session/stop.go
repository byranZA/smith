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
//
// Idempotence is not the same as answering yes to anything, so a name no
// session holds is refused in the same words attach and rm refuse it: tmux
// having nothing to kill is exactly what a typo looks like, and reporting
// success for one would tell the operator they had stopped a session they had
// misspelled. A live tmux session under smith's naming is ended without asking
// the registry, because a process smith can still end is worth ending whether
// or not a worktree is behind it.
func Stop(ctx context.Context, env Env, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("no session named: want the name `session list` reports")
	}
	if !isLive(ctx, env.Tmux, name) {
		_, err := find(ctx, env, name)
		return err
	}
	if _, err := run(ctx, env.Tmux, "tmux", "kill-session", "-t", TmuxSession(name)); err != nil {
		return fmt.Errorf("end the tmux session for %q: %w", name, err)
	}
	return nil
}
