// Package session owns what a session is: one tmux session in one git worktree
// of one repo on one branch. It is the only place that knows how those three
// are wired together, so nothing above it composes a git or tmux command.
//
// The verbs run on the box. What the blueprint decided reaches them as an Env
// the caller resolved — the workspace root and the declared repos — because
// this package never parses a blueprint and never resolves a placement's
// source reference. git and tmux are the system boundary and are injected as
// runners, the same shape internal/connection uses for ssh and scp: a real
// temp-dir repo drives the git paths in tests, while tmux, which needs a
// server and a TTY, is faked at the command boundary.
package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// bareDir is the bare repo's directory name inside a repo's workspace
// directory. The repo is bare so that no branch is the checkout, which is what
// lets every session be a worktree beside it.
const bareDir = "repo.git"

// worktreesDir is the directory holding a repo's worktrees, one level below
// which every session's checkout lives.
const worktreesDir = "worktrees"

// Runner launches a local command. It is the system boundary smith reaches git
// and tmux through, injected so a test can drive real git against a temp
// directory while faking tmux, which needs a server and a TTY.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Repo is a repo the box carries, as the caller resolved it from the
// blueprint: the workspace directory it lives in and the branch its worktrees
// start from.
type Repo struct {
	// Name is the repo's workspace directory name, and the first half of
	// every session name derived for it.
	Name string
	// Base is the branch worktrees start from. Left empty, the repo's own
	// default branch is read from git.
	Base string
	// Placements are the placement to: paths the blueprint declares for this
	// repo, relative to a worktree's root. They are the files smith puts in a
	// worktree itself, and are subtracted from the dirty check: the operator
	// did not write them, so they are not the operator's work.
	Placements []string
}

// Env is what a session verb runs against: the box configuration the caller
// resolved, and the two commands smith drives the box with.
type Env struct {
	// Workspace is the absolute directory on the box the repos live under.
	// The caller resolves it, so this package never expands a ~.
	Workspace string
	// Repos are the repos the blueprint declares. A repo it does not declare
	// is not a repo smith manages.
	Repos []Repo
	// Git runs git.
	Git Runner
	// Tmux runs tmux.
	Tmux Runner
}

// Session is one tmux session in one git worktree — the canonical record every
// session verb answers with, and exactly what `session list` renders.
type Session struct {
	// Name is the derived name every other session verb takes.
	Name string
	// Repo is the repo the worktree is cut from.
	Repo string
	// Branch is the branch the worktree is on, as git holds it rather than as
	// the name spells it.
	Branch string
	// Live reports whether the session's tmux session is running. It is
	// probed, never remembered.
	Live bool
	// Dirty reports whether the worktree holds work that is not committed,
	// ignoring the files smith placed there itself.
	Dirty bool
	// Unpushed counts the branch's commits that no remote-tracking ref
	// reaches.
	Unpushed int
}

// StartRequest is the pair a session is stood up from: the repo the blueprint
// declares and the branch to work on.
type StartRequest struct {
	// Repo names a repo the blueprint declares.
	Repo string
	// Branch is the branch the session works on.
	Branch string
}

// Start ensures a session is running and returns it. It is not a create verb:
// one probe pair — does the worktree exist, and is its tmux session live —
// decides which of three things it does.
//
// With no worktree, the branch is cut from the repo's default branch, a
// worktree for it is created one level below the repo's worktrees directory,
// and a tmux session named smith/<name> is launched in it. With a worktree
// whose tmux session is gone, the tmux session is launched in the checkout the
// operator left: that is how a stopped session resumes, which is why there is
// no separate resume verb. With both already there, Start creates nothing and
// reports the session as it found it.
//
// It stands the session up and returns without connecting to it, which is what
// makes it drivable with no human present.
func Start(ctx context.Context, env Env, req StartRequest) (Session, error) {
	repo, err := env.repo(req.Repo)
	if err != nil {
		return Session{}, err
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return Session{}, fmt.Errorf("no branch named for repo %q: want the branch to work on", repo.Name)
	}

	root := filepath.Join(env.Workspace, repo.Name)
	bare := filepath.Join(root, bareDir)
	name := DeriveName(repo.Name, branch)
	stood := Session{Name: name, Repo: repo.Name, Branch: branch, Live: true}

	existing, found, err := worktreeOn(ctx, env.Git, bare, branch)
	if err != nil {
		return Session{}, err
	}
	if found {
		if isLive(ctx, env.Tmux, name) {
			return stood, nil
		}
		if err := launch(ctx, env.Tmux, name, existing.path); err != nil {
			return Session{}, err
		}
		return stood, nil
	}

	base := repo.Base
	if base == "" {
		if base, err = defaultBranch(ctx, env.Git, bare); err != nil {
			return Session{}, err
		}
	}
	dir := filepath.Join(root, worktreesDir, worktreeDir(branch))
	if _, err := run(ctx, env.Git, "git", "-C", bare,
		"worktree", "add", "-b", branch, dir, base); err != nil {
		return Session{}, fmt.Errorf("create a worktree for branch %q of repo %q at %s: %w", branch, repo.Name, dir, err)
	}
	if err := launch(ctx, env.Tmux, name, dir); err != nil {
		return Session{}, err
	}
	return stood, nil
}

// launch starts the detached tmux session a session runs under, with its
// working directory in the worktree. It is the one place a tmux session is
// created, so the create path and the resume path cannot drift apart.
func launch(ctx context.Context, tmux Runner, name, dir string) error {
	if _, err := run(ctx, tmux, "tmux", "new-session", "-d", "-s", TmuxSession(name), "-c", dir); err != nil {
		return fmt.Errorf("launch the tmux session for %q at %s: %w", name, dir, err)
	}
	return nil
}

// repo finds the declared repo of that name. A repo the blueprint does not
// declare is refused rather than guessed at: smith manages what was declared,
// and a typo that stood a worktree up somewhere unmanaged would be found only
// by the operator hunting for it.
func (e Env) repo(name string) (Repo, error) {
	for _, r := range e.Repos {
		if r.Name == name {
			return r, nil
		}
	}
	return Repo{}, fmt.Errorf("repo %q is not declared in the blueprint this box was built from%s", name, declared(e.Repos))
}

// declared names the repos the blueprint does declare, so a refusal hands the
// operator the set to choose from rather than only the value it rejected.
func declared(repos []Repo) string {
	if len(repos) == 0 {
		return "; it declares no repos at all"
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return "; it declares " + strings.Join(names, ", ")
}

// defaultBranch reads the branch a bare repo's HEAD points at — the branch a
// new one is cut from when the operator names no base.
func defaultBranch(ctx context.Context, git Runner, bare string) (string, error) {
	branch, err := run(ctx, git, "git", "-C", bare, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read the default branch of the repo at %s: %w", bare, err)
	}
	return branch, nil
}

// run launches one command through a runner and answers with its trimmed
// stdout. A command that fails carries its stderr into the error, because what
// git or tmux said is the whole diagnosis and an exit status alone is not.
func run(ctx context.Context, runner Runner, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := runner.Run(ctx, name, args, nil, &stdout, &stderr); err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, said)
		}
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
