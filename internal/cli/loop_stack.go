package cli

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/stack"
)

// stackLeaseKey names the stack lease of the repo dir is in: by its origin, or
// by its git directory when it has none.
func (w loopWiring) stackLeaseKey(ctx context.Context, dir string) (string, error) {
	origin, err := w.gitLine(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		remotes, lerr := w.gitLine(ctx, dir, "remote")
		if lerr != nil {
			return "", fmt.Errorf("list the repo's remotes: %w", lerr)
		}
		if slices.Contains(strings.Fields(remotes), "origin") {
			return "", fmt.Errorf("read the repo's origin: %w", err)
		}
		origin = ""
	}
	common := ""
	if origin == "" {
		var err error
		if common, err = w.gitLine(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err != nil {
			return "", fmt.Errorf("find the repo's git directory: %w", err)
		}
	}
	return stack.Key(origin, common), nil
}

// gitLine runs git with args in dir and returns its trimmed output.
func (w loopWiring) gitLine(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := w.git.Run(ctx, "git", append([]string{"-C", dir}, args...), nil, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// refuseHeldStack refuses, reported on cmd, a run whose settings bring a stack
// up while a live run holds the lease of the repo smith is run from, naming
// the holder. It reads the lease and takes none, so nothing is created. A run
// with no stack, or a wiring with no state home, is never refused.
func (w loopWiring) refuseHeldStack(cmd *cobra.Command, settings loop.Settings) error {
	dir, err := w.workdir()
	if err != nil {
		return reportInvalid(cmd, fmt.Errorf("find the repo to work in: %w", err))
	}
	return w.refuseHeldStackOf(cmd, settings, dir)
}

// refuseHeldStackOf is refuseHeldStack for the repo dir is in.
func (w loopWiring) refuseHeldStackOf(cmd *cobra.Command, settings loop.Settings, dir string) error {
	if !settings.Stacks() || w.state == nil {
		return nil
	}
	state, err := w.state()
	if err != nil {
		return reportInvalid(cmd, err)
	}
	key, err := w.stackLeaseKey(cmd.Context(), dir)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	holder, err := stack.Holder(state, key)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	if holder != nil {
		return reportInvalid(cmd, &stack.HeldError{Record: *holder})
	}
	return nil
}

// stacker returns the stack a run of the work branch brings up and down, with
// the lease of the repo smith is run from. A wiring with no state home takes
// no lease.
func (w loopWiring) stacker(ctx context.Context, branch string) (loop.Stacker, error) {
	if w.state == nil {
		return w.newStack("", "", branch), nil
	}
	state, err := w.state()
	if err != nil {
		return nil, err
	}
	dir, err := w.workdir()
	if err != nil {
		return nil, fmt.Errorf("find the repo to work in: %w", err)
	}
	key, err := w.stackLeaseKey(ctx, dir)
	if err != nil {
		return nil, err
	}
	return w.newStack(state, key, branch), nil
}
