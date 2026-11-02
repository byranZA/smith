// Package checkout resolves the repo files a local loop run places into task
// worktrees from the operator's own checkout.
package checkout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/staging"
)

// Placements returns the placements the config home's blueprints declare for the repo whose origin the checkout containing dir has, or nil when dir has no checkout, origin or declaring blueprint. It returns an error for a failed git probe or a declared source that is missing, tracked or not gitignored, and warns on warn of a blueprint it skips.
func Placements(ctx context.Context, dir string, home config.Home, git loop.Runner, warn io.Writer) (*loop.Placements, error) {
	top, present, err := gitProbe(ctx, git, dir, "not a git repository", "rev-parse", "--show-toplevel")
	if err != nil || !present {
		return nil, err
	}
	origin, present, err := gitProbe(ctx, git, dir, "No such remote", "remote", "get-url", "origin")
	if err != nil || !present {
		return nil, err
	}
	declared, err := config.PlacementsFor(home, origin, warn)
	if err != nil {
		return nil, fmt.Errorf("find the blueprint placements for %s: %w", origin, err)
	}
	if len(declared) == 0 {
		return nil, nil
	}
	placements := make([]blueprint.Placement, len(declared))
	to := make([]string, len(declared))
	for i, d := range declared {
		if err := checkSource(ctx, git, top, d); err != nil {
			return nil, err
		}
		placements[i] = d.Placement
		to[i] = d.Placement.To
	}
	return &loop.Placements{
		Repo:   filepath.Base(top),
		To:     to,
		Placer: placer{checkout: top, placements: placements},
		Git:    git,
	}, nil
}

// checkSource refuses a declared placement whose source is missing from the
// checkout at top, or is a file the repo tracks, which is already in every
// worktree and would carry uncommitted edits into the task if copied, or is
// not gitignored.
func checkSource(ctx context.Context, git loop.Runner, top string, d config.Declared) error {
	source := filepath.Join(top, d.Placement.To)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("placement source %s is missing from the checkout %s, declared by blueprint %s: %w", source, top, d.Blueprint, err)
	}
	tracked, err := gitOutput(ctx, git, top, "ls-files", "--", d.Placement.To)
	if err != nil {
		return fmt.Errorf("check whether placement source %s is tracked: %w", source, err)
	}
	if tracked != "" {
		return fmt.Errorf("placement source %s, declared by blueprint %s, is a tracked file the loop will not copy: it is already in the worktree", source, d.Blueprint)
	}
	if err := git.Run(ctx, "git", []string{"-C", top, "check-ignore", "-q", "--", d.Placement.To}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("placement source %s, declared by blueprint %s, is not gitignored in the checkout: add %s to its .gitignore", source, d.Blueprint, d.Placement.To)
	}
	return nil
}

// TopLevel returns the top level of the checkout containing dir.
func TopLevel(ctx context.Context, git loop.Runner, dir string) (string, error) {
	return gitOutput(ctx, git, dir, "rev-parse", "--show-toplevel")
}

// gitOutput returns the trimmed output of git run in dir with args.
func gitOutput(ctx context.Context, git loop.Runner, dir string, args ...string) (string, error) {
	out, _, err := gitRun(ctx, git, dir, args...)
	return out, err
}

// gitProbe returns the trimmed output of git run in dir with args, and whether
// the thing probed is present. Git exiting non-zero with absent in its
// diagnostic means it is absent and is no error; any other failure is
// returned, so a cancelled or broken run is not mistaken for absence.
func gitProbe(ctx context.Context, git loop.Runner, dir, absent string, args ...string) (string, bool, error) {
	out, stderr, err := gitRun(ctx, git, dir, args...)
	if err == nil {
		return out, true, nil
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && ctx.Err() == nil && strings.Contains(stderr, absent) {
		return "", false, nil
	}
	return "", false, err
}

// gitRun returns the trimmed output and diagnostics of git run in dir with args.
func gitRun(ctx context.Context, git loop.Runner, dir string, args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	if err := git.Run(ctx, "git", append([]string{"-C", dir}, args...), nil, &out, &errOut); err != nil {
		diag := strings.TrimSpace(errOut.String())
		return "", diag, fmt.Errorf("git %s: %w: %s", args[0], err, diag)
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(errOut.String()), nil
}

// placer places declared placements into a worktree by copying the same paths
// from the operator's checkout.
type placer struct {
	// checkout is the top level of the worktree the loop was started in.
	checkout string
	// placements are the files to place.
	placements []blueprint.Placement
}

// Place converges the placements into the worktree at dir from the checkout.
func (p placer) Place(repo, dir string) error {
	if _, err := staging.PlaceFromCheckout(p.checkout, dir, p.placements); err != nil {
		return fmt.Errorf("place the files of repo %q from %s into %s: %w", repo, p.checkout, dir, err)
	}
	return nil
}
