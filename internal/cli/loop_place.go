package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
)

// placements returns the repo files a loop run puts into each task worktree.
// On a box that is what the staged blueprint declares for the repo the run's
// directory is in, placed from the bytes `machine setup` staged; off one,
// where nothing is staged, it is what the config home's blueprints declare for
// the checkout's origin, copied from the checkout. A directory outside the
// box's workspace, or of a repo nothing declares, yields nil, which places
// nothing.
func (w loopWiring) placements(cmd *cobra.Command, ref tracker.Ref) (*loop.Placements, error) {
	if w.box == nil {
		return w.localPlacements(cmd.Context(), cmd.ErrOrStderr())
	}
	inv := invocation(cmd, "loop run", typed(cmd, []string{fmt.Sprint(ref.Number)}))
	box, err := w.box(inv)
	var absent *staging.AbsentError
	if errors.As(err, &absent) {
		return w.localPlacements(cmd.Context(), cmd.ErrOrStderr())
	}
	if err != nil {
		return nil, err
	}
	dir, err := w.workdir()
	if err != nil {
		return nil, fmt.Errorf("find the repo to work in: %w", err)
	}
	ws, err := boxPath(box.Resolution.Workspace)
	if err != nil {
		return nil, err
	}
	return placementsOf(box, repoOf(ws, dir), w, inv), nil
}

// localPlacements returns the placements the config home's blueprints declare
// for the repo whose origin the run's checkout has, to be copied from the top
// level of that checkout, warning on warn of a blueprint it skips. A directory with no top level or origin, or whose
// repo no blueprint declares, yields nil.
func (w loopWiring) localPlacements(ctx context.Context, warn io.Writer) (*loop.Placements, error) {
	dir, err := w.workdir()
	if err != nil {
		return nil, fmt.Errorf("find the repo to work in: %w", err)
	}
	top, err := w.gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil
	}
	origin, err := w.gitOutput(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, nil
	}
	home, err := w.home()
	if err != nil {
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
	for i, d := range declared {
		if err := w.checkSource(ctx, top, d); err != nil {
			return nil, err
		}
		placements[i] = d.Placement
	}
	return &loop.Placements{
		Repo:   filepath.Base(top),
		To:     placedAt(placements),
		Placer: checkoutPlacer{checkout: top, placements: placements},
		Git:    w.git,
	}, nil
}

// checkSource refuses a declared placement whose source is missing from the
// checkout at top, or is a file the repo tracks, which is already in every
// worktree and would carry uncommitted edits into the task if copied.
func (w loopWiring) checkSource(ctx context.Context, top string, d config.Declared) error {
	source := filepath.Join(top, d.Placement.To)
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("placement source %s is missing from the checkout %s, declared by blueprint %s: %w", source, top, d.Blueprint, err)
	}
	tracked, err := w.gitOutput(ctx, top, "ls-files", "--", d.Placement.To)
	if err != nil {
		return fmt.Errorf("check whether placement source %s is tracked: %w", source, err)
	}
	if tracked != "" {
		return fmt.Errorf("placement source %s, declared by blueprint %s, is a tracked file the loop will not copy: it is already in the worktree", source, d.Blueprint)
	}
	if err := w.git.Run(ctx, "git", []string{"-C", top, "check-ignore", "-q", "--", d.Placement.To}, nil, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("placement source %s, declared by blueprint %s, is not gitignored in the checkout: add %s to its .gitignore", source, d.Blueprint, d.Placement.To)
	}
	return nil
}

// gitOutput returns the trimmed output of git run in dir with args.
func (w loopWiring) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := w.git.Run(ctx, "git", append([]string{"-C", dir}, args...), nil, &stdout, &stderr); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// checkoutPlacer places a repo's declared placements into a worktree by
// copying the same paths from the operator's checkout.
type checkoutPlacer struct {
	// checkout is the top level of the worktree the loop was started in.
	checkout string
	// placements are the files to place.
	placements []blueprint.Placement
}

// Place converges the placements into the worktree at dir from the checkout.
func (p checkoutPlacer) Place(repo, dir string) error {
	if _, err := staging.PlaceFromCheckout(p.checkout, dir, p.placements); err != nil {
		return fmt.Errorf("place the files of repo %q from %s into %s: %w", repo, p.checkout, dir, err)
	}
	return nil
}

// placementsOf returns the placements box declares for the repo called name,
// or nil when it declares none.
func placementsOf(box boxConfig, name string, w loopWiring, inv hint.Invocation) *loop.Placements {
	for _, r := range box.Repos {
		if r.Name != name || len(r.Placements) == 0 {
			continue
		}
		return &loop.Placements{
			Repo:   name,
			To:     placedAt(r.Placements),
			Placer: stagedPlacer{root: w.root, repos: box.Repos, hint: inv},
			Git:    w.git,
		}
	}
	return nil
}

// repoOf names the repo of the workspace at ws that dir is in: the first
// directory under ws on the way to dir. It is empty when dir is outside ws.
func repoOf(ws, dir string) string {
	for _, p := range []*string{&ws, &dir} {
		if resolved, err := filepath.EvalSymlinks(*p); err == nil {
			*p = resolved
		}
	}
	rel, err := filepath.Rel(ws, dir)
	if err != nil || rel == "." {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if slices.Contains(parts[:1], "..") {
		return ""
	}
	return parts[0]
}
