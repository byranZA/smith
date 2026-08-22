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
	var sessions []Session
	for _, repo := range env.Repos {
		bare := filepath.Join(env.Workspace, repo.Name, bareDir)
		if _, err := os.Stat(bare); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reach the repo %q at %s: %w", repo.Name, bare, err)
		}
		listing, err := run(ctx, env.Git, "git", "-C", bare, "worktree", "list", "--porcelain")
		if err != nil {
			return nil, fmt.Errorf("list the worktrees of repo %q at %s: %w", repo.Name, bare, err)
		}
		for _, wt := range parseWorktrees(listing) {
			name := listedName(repo.Name, wt)
			sessions = append(sessions, Session{
				Name:   name,
				Repo:   repo.Name,
				Branch: wt.branch,
				Live:   isLive(ctx, env.Tmux, name),
			})
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Repo != sessions[j].Repo {
			return sessions[i].Repo < sessions[j].Repo
		}
		return sessions[i].Branch < sessions[j].Branch
	})
	return sessions, nil
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
