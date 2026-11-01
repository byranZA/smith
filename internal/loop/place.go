package loop

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
)

// Placer materializes a repo's declared placements into a worktree: the files
// smith puts there itself, from the bytes `machine setup` staged on the box.
// It is the same seam the session verbs place through.
type Placer interface {
	// Place converges the named repo's declared placements into the worktree
	// at dir.
	Place(repo, dir string) error
}

// Placements are the files a box puts into each of the loop's task worktrees:
// what Repo declares, placed by Placer at the paths To. A nil *Placements, as
// off a box, places nothing.
type Placements struct {
	// Repo names the repo whose placements these are.
	Repo string
	// To are the destination paths, relative to a worktree.
	To []string
	// Placer writes the files.
	Placer Placer
	// Git is how destinations are checked against the repo's .gitignore.
	Git Runner
}

// into places the repo's files into the task worktree at dir, once every
// destination is known to be gitignored there: a placement at a path git
// would track is a file an agent may commit, or a tracked file overwritten
// into the diff.
func (p *Placements) into(ctx context.Context, dir string) error {
	if p == nil || len(p.To) == 0 {
		return nil
	}
	exposed, err := p.unignored(ctx, dir)
	if err != nil {
		return err
	}
	if len(exposed) > 0 {
		return fmt.Errorf("repo %q places files at paths its .gitignore does not ignore: %s: add them to the repo's .gitignore",
			p.Repo, strings.Join(exposed, ", "))
	}
	if err := p.Placer.Place(p.Repo, dir); err != nil {
		return fmt.Errorf("place the files of repo %q: %w", p.Repo, err)
	}
	return nil
}

// unignored returns the destinations git, run in dir, does not ignore.
func (p *Placements) unignored(ctx context.Context, dir string) ([]string, error) {
	args := append([]string{"-C", dir, "check-ignore", "--"}, p.To...)
	var stdout, stderr bytes.Buffer
	err := p.Git.Run(ctx, "git", args, nil, &stdout, &stderr)
	// check-ignore exits 1 when it ignores none of the paths, which is an
	// answer; only a failure that says something is an error.
	if err != nil && strings.TrimSpace(stderr.String()) != "" {
		return nil, fmt.Errorf("check which placement destinations of repo %q are gitignored: %w: %s", p.Repo, err, strings.TrimSpace(stderr.String()))
	}
	ignored := strings.Fields(stdout.String())
	var exposed []string
	for _, to := range p.To {
		if !slices.Contains(ignored, to) {
			exposed = append(exposed, to)
		}
	}
	return exposed, nil
}
