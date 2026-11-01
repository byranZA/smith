package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/tracker"
)

// placements returns the repo files a loop run puts into each task worktree:
// what the staged blueprint declares for the repo the run's directory is in,
// placed from the bytes `machine setup` staged. Off a box, where nothing is
// staged, or in a directory outside the box's workspace or of a repo the
// blueprint does not declare, it returns nil, which places nothing.
func (w loopWiring) placements(cmd *cobra.Command, ref tracker.Ref) (*loop.Placements, error) {
	if w.box == nil {
		return nil, nil
	}
	inv := invocation(cmd, "loop run", typed(cmd, []string{fmt.Sprint(ref.Number)}))
	box, err := w.box(inv)
	var absent *staging.AbsentError
	if errors.As(err, &absent) {
		return nil, nil
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
