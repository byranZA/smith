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

// Placer materializes into a worktree the repo placements the blueprint
// declares — the files smith puts there itself, from the bytes `machine setup`
// staged on the box. It is injected because reading the staged tree is the
// box's config contract, which this package does not own and never resolves a
// source reference for.
type Placer interface {
	// Place converges the named repo's declared placements into the worktree
	// at dir.
	Place(repo, dir string) error
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
	// Exec replaces smith's own process, which is how attach hands the
	// terminal to tmux.
	Exec Execer
	// Placer materializes a repo's declared placements into a worktree. A nil
	// Placer places nothing, which is what a caller with no staged tree behind
	// it has.
	Placer Placer
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
	// Base is the ref a new branch is cut from, overriding the base the
	// blueprint declares for the repo. It applies only when cutting: against
	// a branch that already exists it is a refusal, because the operator
	// naming a base has a rebase or a reset in mind and smith's git surface
	// is worktree add and worktree remove.
	Base string
}

// Start ensures a session is running and returns it. It is not a create verb:
// it works out what is missing and supplies only that.
//
// It refuses first, with nothing yet on disk, on the four things it cannot
// make right afterwards: a repo the blueprint does not declare, a derived name
// another branch already holds, a base named against a branch that already
// exists, and a branch checked out somewhere that is not a session smith stood
// up. Then it ensures the worktree — checking the branch out into a new one,
// cutting the branch first when the repo does not hold it yet, or taking the
// checkout the operator left. A session already live in that checkout is
// returned as it was found, because swapping a file out from under a running
// process is never wanted, which is what makes stop then start the way to pick
// up changed config. Everything else converges the blueprint's repo placements
// into the worktree and launches a tmux session named smith/<name> in it, so
// start is an ensure-desired-state verb the way `machine setup` is.
//
// A new branch is cut from the requested base, else from the base the
// blueprint declares for the repo, else from the repo's default branch.
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
	stand := standUp{
		repo:   repo,
		bare:   filepath.Join(root, bareDir),
		trees:  filepath.Join(root, worktreesDir),
		name:   DeriveName(repo.Name, branch),
		branch: branch,
		base:   strings.TrimSpace(req.Base),
	}
	stood := Session{Name: stand.name, Repo: repo.Name, Branch: branch, Live: true}

	worktrees, err := listWorktrees(ctx, env.Git, stand.bare)
	if err != nil {
		return Session{}, err
	}
	exists, err := branchExists(ctx, env.Git, stand.bare, branch)
	if err != nil {
		return Session{}, err
	}
	if err := stand.refuse(worktrees, exists); err != nil {
		return Session{}, err
	}

	dir, live, err := stand.worktree(ctx, env, worktrees, exists)
	if err != nil {
		return Session{}, err
	}
	if live {
		return stood, nil
	}
	if err := env.place(repo, dir); err != nil {
		return Session{}, err
	}
	if err := launch(ctx, env.Tmux, stand.name, dir); err != nil {
		return Session{}, err
	}
	return stood, nil
}

// standUp is one Start's request as it resolved: the repo the blueprint
// declares, where on the box that repo keeps its bare clone and its worktrees,
// and the branch and derived name the session is for. It exists so the steps
// of a stand-up — refuse, ensure the worktree, check the branch out — read as
// stages of one request rather than as functions threading six arguments
// between them.
type standUp struct {
	// repo is the declared repo the session's worktree is cut from.
	repo Repo
	// bare is the repo's bare clone, which every git command runs against.
	bare string
	// trees is the repo's worktrees directory, one level below which every
	// session smith stood up lives.
	trees string
	// name is the session's derived name, and the tail of its tmux session.
	name string
	// branch is the branch to work on, as the operator named it.
	branch string
	// base is the ref the operator asked a new branch be cut from, empty
	// when they named none.
	base string
}

// refuse reports the reasons this session must not be stood up. It runs before
// anything is on disk, so a refusal leaves the box exactly as it found it.
func (s standUp) refuse(worktrees []worktree, exists bool) error {
	// This enumeration is the one reason Start looks before it creates: the
	// sanitizer is lossy, so two branches can derive one name, and a session
	// standing up under a name another branch already holds would make every
	// other verb address the wrong worktree. If a future ticket ever makes
	// names reversible, the collision gate below becomes dead code rather
	// than staying quietly correct.
	if err := notColliding(worktrees, s.repo, s.name, s.branch); err != nil {
		return err
	}
	if s.base != "" && exists {
		return fmt.Errorf("branch %q of repo %q already exists, so start cannot base it on %q: start cuts a branch from a base, it never rebases or resets one", s.branch, s.repo.Name, s.base)
	}
	if held, ok := heldBy(worktrees, s.branch); ok && !oneBelow(held.path, s.trees) {
		return fmt.Errorf("branch %q of repo %q is already checked out at %s, held by session %q: git allows one worktree per branch, and that checkout is not one smith stood up below %s", s.branch, s.repo.Name, held.path, listedName(s.repo.Name, held), s.trees)
	}
	return nil
}

// worktree ensures the branch has a checkout of its own and answers with it,
// along with whether the session is already live in it — the one case that
// leaves the worktree untouched, because a running process is in it.
func (s standUp) worktree(ctx context.Context, env Env, worktrees []worktree, exists bool) (string, bool, error) {
	if held, ok := heldBy(worktrees, s.branch); ok {
		return held.path, isLive(ctx, env.Tmux, s.name), nil
	}
	dir := filepath.Join(s.trees, worktreeDir(s.branch))
	if err := s.checkout(ctx, env, dir, exists); err != nil {
		return "", false, err
	}
	return dir, false, nil
}

// checkout puts the branch in a worktree of its own at dir. A branch that
// already exists — the one a reclaimed session left behind in the bare repo —
// is checked out as it stands; a branch that does not is cut, and only that
// path fetches. A branch cut from a stale base is a merge-time failure that
// looks like anything but a session bug, and cutting is the one moment smith
// is already touching the network, so the resume and connect paths pay
// nothing for it.
func (s standUp) checkout(ctx context.Context, env Env, dir string, exists bool) error {
	args := []string{"-C", s.bare, "worktree", "add", dir, s.branch}
	if !exists {
		if err := fetch(ctx, env.Git, s.bare, s.repo.Name); err != nil {
			return err
		}
		base := s.base
		if base == "" {
			base = s.repo.Base
		}
		if base == "" {
			var err error
			if base, err = defaultBranch(ctx, env.Git, s.bare); err != nil {
				return err
			}
		}
		args = []string{"-C", s.bare, "worktree", "add", "-b", s.branch, dir, base}
	}
	if _, err := run(ctx, env.Git, "git", args...); err != nil {
		return fmt.Errorf("create a worktree for branch %q of repo %q at %s: %w", s.branch, s.repo.Name, dir, err)
	}
	return nil
}

// fetch refreshes the repo from its remote. It runs only where a branch is
// about to be cut, and it also keeps the unpushed count honest: that count
// reads the remote-tracking refs, so it is only as fresh as the last fetch.
func fetch(ctx context.Context, git Runner, bare, repo string) error {
	if _, err := run(ctx, git, "git", "-C", bare, "fetch"); err != nil {
		return fmt.Errorf("fetch the repo %q at %s before cutting a branch: %w", repo, bare, err)
	}
	return nil
}

// branchExists reports whether the bare repo already holds that branch. It
// asks for the branch by name rather than reading a ref path, so a branch
// that is simply absent is an empty answer and not an error to be told apart
// from a repo that could not be read.
func branchExists(ctx context.Context, git Runner, bare, branch string) (bool, error) {
	listed, err := run(ctx, git, "git", "-C", bare, "branch", "--list", branch, "--format=%(refname:short)")
	if err != nil {
		return false, fmt.Errorf("look for branch %q in the repo at %s: %w", branch, bare, err)
	}
	return listed != "", nil
}

// notColliding refuses when the name this session would take is already held
// by a worktree on a different branch. Both branches are named: the derived
// name alone cannot tell the operator which of their branches is in the way,
// because deriving it is what lost the difference.
func notColliding(worktrees []worktree, repo Repo, name, branch string) error {
	for _, wt := range worktrees {
		if wt.branch != branch && listedName(repo.Name, wt) == name {
			return fmt.Errorf("session name %q is already held by branch %q of repo %q, which branch %q derives the same name as: rename one of the two branches", name, wt.branch, repo.Name, branch)
		}
	}
	return nil
}

// heldBy finds the worktree the branch is checked out in, if any.
func heldBy(worktrees []worktree, branch string) (worktree, bool) {
	for _, wt := range worktrees {
		if wt.branch == branch {
			return wt, true
		}
	}
	return worktree{}, false
}

// oneBelow reports whether path is a checkout directly below dir, which is
// the layout every session smith stands up has. Symlinks are resolved because
// a temp directory is often reached through one and git reports the path it
// resolved to.
func oneBelow(path, dir string) bool {
	return resolved(filepath.Dir(path)) == resolved(dir)
}

// resolved is path with its symlinks followed, or cleaned as given when it
// cannot be reached — a path that does not exist is still comparable.
func resolved(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// launch starts the detached tmux session a session runs under, with its
// working directory in the worktree. It is the one place a tmux session is
// created.
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

// place converges the repo's declared placements into the worktree at dir. It
// runs where a session stands up and on neither the connect path nor any read
// verb, so a file only ever moves at the moment nothing is running against
// it.
func (e Env) place(repo Repo, dir string) error {
	if e.Placer == nil {
		return nil
	}
	if err := e.Placer.Place(repo.Name, dir); err != nil {
		return fmt.Errorf("converge the placements of repo %q into the worktree at %s: %w", repo.Name, dir, err)
	}
	return nil
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
	out, err := runRaw(ctx, runner, name, args...)
	return strings.TrimSpace(out), err
}

// runRaw is run without the trim: it answers with stdout exactly as the
// command wrote it. It exists for the machine-readable reports, where the
// first byte can be significant — a `git status -z` entry's status code
// begins with a space whenever the change is in the checkout rather than the
// index, and trimming it would read that entry as a staged one and take its
// path apart one byte off.
func runRaw(ctx context.Context, runner Runner, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := runner.Run(ctx, name, args, nil, &stdout, &stderr); err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, said)
		}
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
