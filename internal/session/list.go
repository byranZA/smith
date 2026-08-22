package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Filter narrows what List enumerates. It is empty for now — every session on
// the box is listed — and the zero value is the whole listing.
type Filter struct{}

// List enumerates the sessions on the box, sorted by repo and then by branch.
//
// The bare repos are the registry, not the filesystem: a repo is asked for its
// worktrees rather than the worktrees directory being walked, because the
// registry hands back the branch git holds — the directory name is lossy by
// construction — and it draws the line between a session and debris. A
// directory below the worktrees directory that git does not know about is not
// a session, and a row invented for it would make list disagree with rm.
//
// Every row's running state is probed here and now. A declared repo the
// workspace stage has not cloned yet contributes nothing rather than failing
// the listing, because list answers with what is on the box and there is
// nothing on the box to answer with.
func List(ctx context.Context, env Env, _ Filter) ([]Session, error) {
	worktrees, err := worktreesOf(ctx, env)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, p := range worktrees {
		name := listedName(p.repo.Name, p.wt)
		dirty, unpushed, err := workState(ctx, env.Git, p.wt, p.repo.Placements)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, Session{
			Name:     name,
			Repo:     p.repo.Name,
			Branch:   p.wt.branch,
			Live:     isLive(ctx, env.Tmux, name),
			Dirty:    dirty.any(),
			Unpushed: unpushed,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Repo != sessions[j].Repo {
			return sessions[i].Repo < sessions[j].Repo
		}
		return sessions[i].Branch < sessions[j].Branch
	})
	return sessions, nil
}

// placed is one worktree together with the repo whose registry holds it, which
// is what every verb that addresses a session by name needs: the name alone is
// lossy, and the repo is half of what it was derived from.
type placed struct {
	repo Repo
	wt   worktree
}

// worktreesOf enumerates the worktrees the box's bare repos hold, which are
// the sessions on it. It is the one enumeration every verb that works from a
// name shares, so no two of them can disagree about what a session is.
//
// A declared repo the workspace stage has not cloned yet contributes nothing
// rather than failing the enumeration, because there is nothing on the box to
// answer with.
func worktreesOf(ctx context.Context, env Env) ([]placed, error) {
	var found []placed
	for _, repo := range env.Repos {
		bare := filepath.Join(env.Workspace, repo.Name, bareDir)
		if _, err := os.Stat(bare); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reach the repo %q at %s: %w", repo.Name, bare, err)
		}
		worktrees, err := listWorktrees(ctx, env.Git, bare)
		if err != nil {
			return nil, fmt.Errorf("list the worktrees of repo %q: %w", repo.Name, err)
		}
		for _, wt := range worktrees {
			found = append(found, placed{repo: repo, wt: wt})
		}
	}
	return found, nil
}

// worktree is one entry of a bare repo's worktree registry: where the checkout
// is and, unless its head is detached, the branch it is on.
type worktree struct {
	path   string
	branch string
}

// parseWorktrees reads `git worktree list --porcelain` into the worktrees that
// are sessions. The bare repo lists itself and is dropped: it is the registry,
// not a checkout anyone works in.
func parseWorktrees(listing string) []worktree {
	var worktrees []worktree
	var current worktree
	var bare bool
	flush := func() {
		if current.path != "" && !bare {
			worktrees = append(worktrees, current)
		}
		current, bare = worktree{}, false
	}
	for _, line := range strings.Split(listing, "\n") {
		switch field, value, _ := strings.Cut(strings.TrimSpace(line), " "); field {
		case "worktree":
			flush()
			current.path = value
		case "branch":
			current.branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			bare = true
		}
	}
	flush()
	return worktrees
}

// listWorktrees reads the repo's worktree registry.
//
// The registry is asked rather than the worktrees directory being walked: it
// hands back the branch git holds, which the directory name is too lossy to
// carry, and it draws the line between a session and debris.
func listWorktrees(ctx context.Context, git Runner, bare string) ([]worktree, error) {
	listing, err := run(ctx, git, "git", "-C", bare, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list the worktrees of the repo at %s: %w", bare, err)
	}
	return parseWorktrees(listing), nil
}

// listedName is the name a worktree is listed under: the sanitized name the
// other session verbs take. A detached worktree has no branch to derive one
// from, so its name comes from the directory that branch's sanitization
// already produced, which is the same string either way.
func listedName(repo string, wt worktree) string {
	if wt.branch != "" {
		return DeriveName(repo, wt.branch)
	}
	return DeriveName(repo, filepath.Base(wt.path))
}

// isLive probes whether a session's tmux session is running. It is asked on
// every listing and never remembered: there is no daemon to remember it, and a
// tmux server killed outside smith would make a stored answer a lie. tmux
// declining to answer is the answer — the session is not running.
func isLive(ctx context.Context, tmux Runner, name string) bool {
	_, err := run(ctx, tmux, "tmux", "has-session", "-t", TmuxSession(name))
	return err == nil
}
