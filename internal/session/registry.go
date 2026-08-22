package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// This file is the worktree registry: the one place that turns the bare repos
// on the box into the sessions on it, and a session name back into the repo
// and branch behind it. Every verb resolves a name and probes running state
// through here, so no two of them can disagree about what a session is or
// which worktree a name addresses.
//
// The registry is git's, not the filesystem's. A repo is asked for its
// worktrees rather than the worktrees directory being walked: the registry
// hands back the branch git holds, which the sanitized directory name is too
// lossy to carry, and it draws the line between a session and debris. A
// directory below the worktrees directory that git does not know about is not
// a session.

// worktree is one entry of a bare repo's worktree registry: where the checkout
// is and, unless its head is detached, the branch it is on.
type worktree struct {
	path   string
	branch string
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

// listWorktrees reads one bare repo's worktree registry. It is what stands a
// session up reads: start already knows the single repo it was handed, so it
// pays for that repo's registry and no other.
func listWorktrees(ctx context.Context, git Runner, bare string) ([]worktree, error) {
	listing, err := run(ctx, git, "git", "-C", bare, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list the worktrees of the repo at %s: %w", bare, err)
	}
	return parseWorktrees(listing), nil
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

// find locates the session of that name among the worktrees the bare repos
// hold. The registry is asked rather than the name being taken apart: the
// derivation is lossy, so the repo and the branch behind a name are only ever
// recoverable from git.
func find(ctx context.Context, env Env, name string) (placed, error) {
	worktrees, err := worktreesOf(ctx, env)
	if err != nil {
		return placed{}, err
	}
	found, ok := lookup(worktrees, name)
	if !ok {
		return placed{}, unknownSession(name)
	}
	return found, nil
}

// lookup picks the worktree listed under that name out of an enumeration
// already in hand, which is what a verb addressing several names at once needs
// so it reads the registry once rather than once per name.
func lookup(worktrees []placed, name string) (placed, bool) {
	for _, p := range worktrees {
		if listedName(p.repo.Name, p.wt) == name {
			return p, true
		}
	}
	return placed{}, false
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

// unknownSession is the refusal for a name no session holds. It is one string
// wherever a name is resolved, so a typo reads the same at every verb.
func unknownSession(name string) error {
	return fmt.Errorf("no session named %q exists on this box: `smith session list` reports the sessions there are", name)
}

// isLive probes whether a session's tmux session is running. It is asked on
// every listing and never remembered: there is no daemon to remember it, and a
// tmux server killed outside smith would make a stored answer a lie. tmux
// declining to answer is the answer — the session is not running.
func isLive(ctx context.Context, tmux Runner, name string) bool {
	_, err := run(ctx, tmux, "tmux", "has-session", "-t", TmuxSession(name))
	return err == nil
}
