package workspace

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
)

// Where a declared repo lands on the box.
const (
	// cloneDir is the bare clone's directory inside a repo's own directory,
	// with worktrees/ beside it for the sessions a later stage stands up.
	cloneDir = "repo.git"
	// repoMode is the permission of the directory a clone is made under: the
	// smith user's own, and no wider.
	repoMode = 0o700
	// trackingRefspec is the fetch refspec that keeps a clone's
	// refs/remotes/origin/* current. A bare clone sets none, so without it a
	// fetch moves only FETCH_HEAD and the remote's branches are never seen.
	trackingRefspec = "+refs/heads/*:refs/remotes/origin/*"
)

// Repo is one declared repository of the plan: the remote it is cloned from
// and where its bare clone lands on the box.
//
// The clone is bare deliberately. A normal checkout makes one branch
// permanently the checked-out one, which therefore cannot also be a worktree —
// a special case both a human and an agent loop would have to route around.
// Bare deletes it, so every branch is reached identically.
type Repo struct {
	// Name is the directory the repo occupies under the workspace root,
	// defaulted from the url when the blueprint left it out.
	Name string
	// URL is the remote the clone is made from, and the one an existing clone
	// is held to.
	URL string
	// Path is where the bare clone lands, absolute.
	Path string
	// Mise is where mise is installed if the box does not already have it. The
	// clone runs under whichever mise the run proves — this one, or the one
	// the box already answers by — so that the blueprint's box-level env is in
	// scope for a token-authenticated clone.
	Mise string
}

// convergeRepo leaves the box holding a bare clone of the repo and reports what
// it did.
//
// A repo the box does not have yet is cloned. One it already has is fetched,
// after its tracking refspec is added if an earlier run cloned it without one,
// and is never deleted and cloned again — the clone may already carry
// worktrees and work that is not pushed anywhere. A clone whose remote no longer matches the
// blueprint is refused for that repo and reported rather than repointed:
// silently repointing a bare repo that already has worktrees would change what
// every existing branch tracks, so moving or removing the directory is the
// operator's call.
//
// Every git command runs under mise, because the toolchain step exports the
// blueprint's env through the config mise reads and a forge token in scope is
// what authenticates a clone of a private repo. No worktree is made and no
// repo-scoped placement is written: both belong to the session that stands a
// worktree up.
func convergeRepo(ctx context.Context, run Runner, m *mise, r Repo, progress io.Writer) (string, error) {
	exe, done, err := m.ensure(ctx, run, r.Mise, progress)
	if err != nil {
		return "", err
	}
	exists, err := clonedAt(r.Path)
	if err != nil {
		return "", err
	}
	if !exists {
		cloned, err := clone(ctx, run, exe, r, progress)
		return summarize(done, cloned, err)
	}
	remote, err := remoteURL(ctx, run, exe, r)
	if err != nil {
		return "", err
	}
	if remote != r.URL {
		return "", fmt.Errorf("the clone at %s is of %s, not the %s the blueprint declares: "+
			"move or remove that directory to clone the declared url", r.Path, remote, r.URL)
	}
	repaired, err := track(ctx, run, exe, r, progress)
	if err != nil {
		return "", err
	}
	what := fmt.Sprintf("fetch %s into %s", r.URL, r.Path)
	if err := git(ctx, run, exe, progress, what, "--git-dir", r.Path, "fetch", "origin"); err != nil {
		return "", err
	}
	return summarize(append(done, repaired...), "fetched "+r.Path, nil)
}

// track adds the tracking refspec to a clone that lacks it and reports the
// repair, or nothing when the clone already had it. The clone's local branches
// are left where they are: worktrees may have them checked out.
func track(ctx context.Context, run Runner, exe string, r Repo, progress io.Writer) ([]string, error) {
	var out bytes.Buffer
	args := miseArgs("--git-dir", r.Path, "config", "--get-all", "remote.origin.fetch")
	if err := run.Run(ctx, exe, args, nil, &out, io.Discard); err != nil && !unset(err) {
		return nil, fmt.Errorf("read the fetch refspecs of the clone at %s: %w", r.Path, err)
	}
	if slices.Contains(strings.Fields(out.String()), trackingRefspec) {
		return nil, nil
	}
	what := fmt.Sprintf("add the remote-tracking refspec to %s", r.Path)
	if err := git(ctx, run, exe, progress, what, "--git-dir", r.Path, "config", "--add", "remote.origin.fetch", trackingRefspec); err != nil {
		return nil, err
	}
	return []string{"added the remote-tracking refspec to " + r.Path}, nil
}

// summarize puts what proving mise took in front of what the repo's own step
// did. It is empty on every step after the one that proved it, so the operator
// is told about an install once and reads the clones plainly.
func summarize(done []string, summary string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return strings.Join(append(done, summary), "; "), nil
}

// clone makes the bare clone with its tracking refspec, having first made the
// repo's own directory so that it carries the smith user's permissions rather
// than whatever git would have left there.
func clone(ctx context.Context, run Runner, exe string, r Repo, progress io.Writer) (string, error) {
	if err := os.MkdirAll(filepath.Dir(r.Path), repoMode); err != nil {
		return "", fmt.Errorf("make the directory above %s: %w", r.Path, err)
	}
	what := fmt.Sprintf("clone %s into %s", r.URL, r.Path)
	if err := git(ctx, run, exe, progress, what, "clone", "--bare", "-c", "remote.origin.fetch="+trackingRefspec, r.URL, r.Path); err != nil {
		return "", err
	}
	return "cloned " + r.URL + " into " + r.Path, nil
}

// remoteURL is the url the box's existing clone was made from, which is what
// the blueprint's declaration is held against.
func remoteURL(ctx context.Context, run Runner, exe string, r Repo) (string, error) {
	var out bytes.Buffer
	args := miseArgs("--git-dir", r.Path, "remote", "get-url", "origin")
	if err := run.Run(ctx, exe, args, nil, &out, io.Discard); err != nil {
		return "", fmt.Errorf("read the remote of the clone at %s: %w", r.Path, err)
	}
	return strings.TrimSpace(out.String()), nil
}

// git runs one git command under mise, streaming its output as it goes so a
// long clone reports on itself rather than looking hung. A failure is reported
// as what the command was for, since the operator reads the outcome and not
// the argv.
func git(ctx context.Context, run Runner, exe string, progress io.Writer, what string, args ...string) error {
	if err := run.Run(ctx, exe, miseArgs(args...), nil, progress, progress); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// miseArgs is a git invocation as mise runs it. It is `mise exec`, never `mise
// activate`, which does not fire in the non-interactive shells smith runs in.
func miseArgs(args ...string) []string {
	return append([]string{"exec", "--", "git"}, args...)
}

// clonedAt reports whether the box already holds a clone at path. An absent
// path is the ordinary first-run case rather than a failure; a plain file
// there is neither, and is reported rather than cloned over.
func clonedAt(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read what the box holds at %s: %w", path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is a file, not a clone: move it out of the way", path)
	}
	return true, nil
}

// unsetStatus is the status `git config --get-all` exits with for a key the
// repo's config does not set.
const unsetStatus = 1

// unset reports whether err is git config answering that the key is not set,
// which mise exec passes through as its own exit status.
func unset(err error) bool {
	var exit interface{ ExitCode() int }
	return errors.As(err, &exit) && exit.ExitCode() == unsetStatus
}
