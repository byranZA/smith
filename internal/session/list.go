package session

import (
	"context"
	"sort"
)

// Filter narrows what List enumerates. The zero value is the whole listing:
// every declared repo, live and stopped alike.
//
// Filters live on the listing and nowhere else. The removal verb takes names,
// because a filter there would make the target set of a delete implicit, and a
// delete has no undo.
type Filter struct {
	// Repo narrows the listing to one repo the blueprint declares. Empty
	// lists every repo.
	Repo string
	// State narrows the listing to one running state. The zero value lists
	// both.
	State State
}

// State is the running state a listing can be narrowed to. Its zero value
// narrows nothing, so a caller that does not care never has to say so.
type State int

const (
	// AnyState lists live and stopped sessions alike.
	AnyState State = iota
	// LiveOnly lists only the sessions whose tmux session is running.
	LiveOnly
	// StoppedOnly lists only the sessions whose tmux session is not running.
	StoppedOnly
)

// wants reports whether a repo survives the filter.
func (f Filter) wants(repo string) bool {
	return f.Repo == "" || f.Repo == repo
}

// running reports whether a session in that running state survives the filter.
// It is asked after the live probe and before the work state is read, because
// the probe is one command and the work state is three.
func (f Filter) running(live bool) bool {
	switch f.State {
	case LiveOnly:
		return live
	case StoppedOnly:
		return !live
	default:
		return true
	}
}

// List enumerates the sessions on the box the filter admits, sorted by repo
// and then by branch.
//
// It reads the worktree registry every other verb reads, so a row it prints is
// a session rm can address and a row it withholds is one no verb can: a
// directory below the worktrees directory that git does not know about is
// debris, not a session, and a row invented for it would make list disagree
// with rm.
//
// Every row's running state is probed here and now. A declared repo the
// workspace stage has not cloned yet contributes nothing rather than failing
// the listing, because list answers with what is on the box and there is
// nothing on the box to answer with.
func List(ctx context.Context, env Env, filter Filter) ([]Session, error) {
	if filter.Repo != "" {
		if _, err := env.repo(filter.Repo); err != nil {
			return nil, err
		}
	}
	worktrees, err := worktreesOf(ctx, env)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, p := range worktrees {
		if !filter.wants(p.repo.Name) {
			continue
		}
		name := listedName(p.repo.Name, p.wt)
		live := isLive(ctx, env.Tmux, name)
		if !filter.running(live) {
			continue
		}
		dirty, unpushed, err := workState(ctx, env.Git, p.wt, p.repo.Placements)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, Session{
			Name:     name,
			Repo:     p.repo.Name,
			Branch:   p.wt.branch,
			Live:     live,
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
