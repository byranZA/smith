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

// BatchRefusedError is what Remove answers with when any of the names it was
// handed does not survive the gate. It carries every offender rather than the
// first: a report that named one would turn a batch into a queue of retries,
// and the operator would learn the shape of the gate one refusal at a time.
type BatchRefusedError struct {
	// Names are the sessions the batch addressed, in the order the operator
	// named them.
	Names []string
	// Refusals are the offenders, in the order they were named — a
	// *RemovalRefusedError for a session that is live or dirty, and a
	// plain error for a name no session holds.
	Refusals []error
}

// Error implements error, rendering every offender's own refusal and, for a
// batch of more than one, the all-or-nothing clause that says the rest
// survived too.
func (e *BatchRefusedError) Error() string {
	said := make([]string, len(e.Refusals))
	for i, refusal := range e.Refusals {
		said[i] = refusal.Error()
	}
	if len(e.Names) > 1 {
		said = append(said, fmt.Sprintf("nothing was removed: `smith session rm` is all-or-nothing across the %d sessions it was handed", len(e.Names)))
	}
	return strings.Join(said, "\n")
}

// Unwrap exposes the offenders to errors.Is and errors.As, so a caller can ask
// what kind of refusal a batch met without taking its message apart.
func (e *BatchRefusedError) Unwrap() []error { return e.Refusals }

// Remove reclaims the worktrees of the named sessions and keeps their
// branches.
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
// force overrides both refusals with one flag: it ends the tmux sessions that
// are running, and reclaims the worktrees regardless of what is uncommitted in
// them. Nothing here ever prompts — a delete has no undo, and a y/N at a delete
// prompt is answered reflexively — so a refusal prints what would die and the
// command that overrides it, identically with a human present and without.
//
// The batch is all-or-nothing and the gate is a property of the operation
// rather than of each name: every name is resolved and gated before any
// worktree is touched, a name no session holds is a refusal like any other,
// and one offender refuses the lot. Partial deletion is the state nobody can
// reason about afterwards, and it would leave --force covering half a batch.
//
// The dirty half is the work-state predicate `session list` renders, asked
// once here rather than defined a second time: two answers to "is this
// worktree dirty" would let list say clean while rm refuses.
func Remove(ctx context.Context, env Env, names []string, force bool) ([]Removal, error) {
	names, err := named(names)
	if err != nil {
		return nil, err
	}
	gated, err := gate(ctx, env, names, force)
	if err != nil {
		return nil, err
	}
	removals := make([]Removal, len(gated))
	for i, g := range gated {
		if removals[i], err = reclaim(ctx, env, g); err != nil {
			return nil, err
		}
	}
	return removals, nil
}

// named is the batch's name list with the surrounding whitespace gone and the
// repeats dropped. A name given twice is not a refusal but it cannot be
// removed twice either: the second pass would fail against a worktree the
// first already reclaimed, which is the partial state the batch exists to
// avoid.
func named(names []string) ([]string, error) {
	var kept []string
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		kept = append(kept, name)
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("no session named: want the names `smith session list` reports")
	}
	return kept, nil
}

// admitted is one name the gate let through, together with what it read to
// decide: the worktree to reclaim, whether a driver has to be ended first, and
// the count the report carries.
type admitted struct {
	name     string
	found    placed
	live     bool
	unpushed int
}

// gate resolves and checks every name before a single worktree is touched. It
// is where all-or-nothing lives, which is why it answers with the whole batch
// or with none of it.
func gate(ctx context.Context, env Env, names []string, force bool) ([]admitted, error) {
	worktrees, err := worktreesOf(ctx, env)
	if err != nil {
		return nil, err
	}
	var gated []admitted
	var refusals []error
	for _, name := range names {
		found, ok := lookup(worktrees, name)
		if !ok {
			refusals = append(refusals, unknownSession(name))
			continue
		}
		live := isLive(ctx, env.Tmux, name)
		dirty, unpushed, err := workState(ctx, env.Git, found.wt, found.repo.Placements)
		if err != nil {
			return nil, err
		}
		if !force && (live || dirty.any()) {
			refusals = append(refusals, &RemovalRefusedError{
				Name:      name,
				Live:      live,
				Modified:  dirty.modified,
				Staged:    dirty.staged,
				Untracked: dirty.untracked,
				Unpushed:  unpushed,
			})
			continue
		}
		gated = append(gated, admitted{name: name, found: found, live: live, unpushed: unpushed})
	}
	if len(refusals) > 0 {
		return nil, &BatchRefusedError{Names: names, Refusals: refusals}
	}
	return gated, nil
}

// reclaim removes one gated session's worktree, ending its tmux session first
// when a forced removal found one running.
func reclaim(ctx context.Context, env Env, g admitted) (Removal, error) {
	if g.live {
		if err := Stop(ctx, env, g.name); err != nil {
			return Removal{}, err
		}
	}
	// git's own refusal is overridden unconditionally, because smith has
	// already asked the only question that decides this: git counts the files
	// smith placed as untracked work, and the predicate above does not.
	bare := filepath.Join(env.Workspace, g.found.repo.Name, bareDir)
	if _, err := run(ctx, env.Git, "git", "-C", bare, "worktree", "remove", "--force", g.found.wt.path); err != nil {
		return Removal{}, fmt.Errorf("reclaim the worktree of session %q at %s: %w", g.name, g.found.wt.path, err)
	}
	return Removal{
		Name:     g.name,
		Repo:     g.found.repo.Name,
		Branch:   g.found.wt.branch,
		Unpushed: g.unpushed,
		Killed:   g.live,
	}, nil
}
