package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Removal is what Remove reclaimed: the session it addressed, the branch that
// survived it, what of that branch no remote has, and whether a running driver
// was ended to get there.
type Removal struct {
	// Name is the session that was removed.
	Name string
	// Repo is the repo whose worktree was reclaimed.
	Repo string
	// Branch is the branch the worktree was on, and which the removal left in
	// the repo.
	Branch string
	// Unpushed counts the branch's commits that no remote-tracking ref
	// reaches. It is reported, never a reason to refuse.
	Unpushed int
	// Killed reports whether a live tmux session was ended, which only a
	// forced removal ever does.
	Killed bool
}

// RemovalRefusedError is what Remove answers with when a session is not safe
// to reclaim. It carries each refusal separately rather than one collapsed
// verdict: a running driver is a process the operator would be killing and an
// uncommitted file is a different kind of loss, and an operator acts on the
// two differently.
type RemovalRefusedError struct {
	// Name is the session that was refused.
	Name string
	// Live reports whether the session's tmux session is running, which is
	// the first of the two refusals.
	Live bool
	// Modified, Staged and Untracked count the uncommitted work the worktree
	// holds, less the files smith placed there itself. Any of them above zero
	// is the second refusal.
	Modified, Staged, Untracked int
	// Unpushed counts the commits no remote has. It is carried as
	// information and is never itself a refusal.
	Unpushed int
}

// Error implements error, rendering one refusal per line — the form the
// command surface prints verbatim.
func (e *RemovalRefusedError) Error() string {
	var refusals []string
	if e.Live {
		refusals = append(refusals, fmt.Sprintf("session `%s` is running — `smith session stop %s` first", TmuxSession(e.Name), e.Name))
	}
	if e.Dirty() {
		refusals = append(refusals, fmt.Sprintf("worktree of session %q holds uncommitted work: %d modified, %d staged, %d untracked%s; reclaim it and lose that work with `smith session rm %s --force`",
			e.Name, e.Modified, e.Staged, e.Untracked, kept(e.Unpushed), e.Name))
	}
	return strings.Join(refusals, "\n")
}

// Dirty reports whether the worktree holding uncommitted work is one of the
// refusals.
func (e *RemovalRefusedError) Dirty() bool {
	return e.Modified+e.Staged+e.Untracked > 0
}

// kept renders the unpushed count as the clause that follows an enumeration,
// and as nothing at all when there is no count to make.
func kept(unpushed int) string {
	if unpushed == 0 {
		return ""
	}
	if unpushed == 1 {
		return ", and 1 commit not on any remote"
	}
	return fmt.Sprintf(", and %d commits not on any remote", unpushed)
}

// Remove reclaims a session's worktree and keeps its branch.
//
// It refuses on a dirty worktree or a live session, and on nothing else. That
// is not a compromise between safety and convenience: the repo is bare and the
// branch survives, so what removal deletes is a working directory that `start`
// puts back with its commits and its placed files. The gate covers precisely
// the part that does not come back.
//
// Unpushed commits are reported and never gate. A check that fires on nearly
// every in-progress branch would train force into muscle memory, which is how
// the dirty gate that does matter gets waved away too.
//
// force overrides both refusals with one flag: it ends the tmux session if one
// is running, and reclaims the worktree regardless of what is uncommitted in
// it. Nothing here ever prompts — a delete has no undo, and a y/N at a delete
// prompt is answered reflexively — so a refusal prints what would die and the
// command that overrides it, identically with a human present and without.
//
// The dirty half is the work-state predicate `session list` renders, asked
// once here rather than defined a second time: two answers to "is this
// worktree dirty" would let list say clean while rm refuses.
func Remove(ctx context.Context, env Env, name string, force bool) (Removal, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Removal{}, fmt.Errorf("no session named: want the name `session list` reports")
	}
	found, err := find(ctx, env, name)
	if err != nil {
		return Removal{}, err
	}
	live := isLive(ctx, env.Tmux, name)
	dirty, unpushed, err := workState(ctx, env.Git, found.wt, found.repo.Placements)
	if err != nil {
		return Removal{}, err
	}
	if !force && (live || dirty.any()) {
		return Removal{}, &RemovalRefusedError{
			Name:      name,
			Live:      live,
			Modified:  dirty.modified,
			Staged:    dirty.staged,
			Untracked: dirty.untracked,
			Unpushed:  unpushed,
		}
	}
	if live {
		if err := Stop(ctx, env, name); err != nil {
			return Removal{}, err
		}
	}
	// git's own refusal is overridden unconditionally, because smith has
	// already asked the only question that decides this: git counts the files
	// smith placed as untracked work, and the predicate above does not.
	bare := filepath.Join(env.Workspace, found.repo.Name, bareDir)
	if _, err := run(ctx, env.Git, "git", "-C", bare, "worktree", "remove", "--force", found.wt.path); err != nil {
		return Removal{}, fmt.Errorf("reclaim the worktree of session %q at %s: %w", name, found.wt.path, err)
	}
	return Removal{
		Name:     name,
		Repo:     found.repo.Name,
		Branch:   found.wt.branch,
		Unpushed: unpushed,
		Killed:   live,
	}, nil
}
