package session

import (
	"context"
	"fmt"
	"strings"
)

// Source is where the root process of a session on a branch will read the
// repo's files from: the checkout the branch is already in, else a commit of
// the repo's bare clone that the branch's new checkout will hold.
type Source struct {
	// Dir is the checkout the branch is already in, as git reports it, and
	// empty when it has none.
	Dir string
	// Ref is the commit, in the repo's bare clone, a new checkout of the
	// branch will hold. It is empty when Dir is set.
	Ref string
	// Cut reports whether the branch does not exist yet, so Ref is the
	// commit Start will cut it from.
	Cut bool
}

// SourceOf answers with where a session Start stands up for req would read
// the repo's files from, refusing what Start would refuse before anything is
// on disk. A branch with a checkout is read from that checkout, one the bare
// clone holds from its tip, and a new branch, or a request naming no branch
// yet, from the fetched base Start would cut it from. It creates no worktree
// and no branch; a new branch's fetch is all it changes.
func SourceOf(ctx context.Context, env Env, req StartRequest) (Source, error) {
	repo, err := env.repo(req.Repo)
	if err != nil {
		return Source{}, err
	}
	branch := strings.TrimSpace(req.Branch)
	stand := newStandUp(env, repo, branch, req.Base)
	exists := false
	if branch != "" {
		worktrees, err := listWorktrees(ctx, env.Git, stand.bare)
		if err != nil {
			return Source{}, err
		}
		if exists, err = branchExists(ctx, env.Git, stand.bare, branch); err != nil {
			return Source{}, err
		}
		if err := stand.refuse(worktrees, exists); err != nil {
			return Source{}, err
		}
		if held, ok := heldBy(worktrees, branch); ok {
			return Source{Dir: held.path}, nil
		}
	}
	ref := "refs/heads/" + branch
	if !exists {
		if ref, err = stand.cutFrom(ctx, env); err != nil {
			return Source{}, err
		}
	}
	commit, err := run(ctx, env.Git, "git", "-C", stand.bare, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return Source{}, fmt.Errorf("resolve %s in the repo at %s: %w", ref, stand.bare, err)
	}
	return Source{Ref: commit, Cut: !exists}, nil
}
