// Package loop works a spec's tasks one at a time: it decides which task is
// next and what remains, from what the tracker says about the spec.
package loop

import (
	"slices"

	"github.com/byranZA/smith/internal/tracker"
)

// Remaining is a spec's tasks sorted by where they stand, each list in the
// spec's own order.
type Remaining struct {
	// Available tasks are open, meant for an agent, and blocked by nothing open.
	Available []tracker.Task
	// Blocked tasks are meant for an agent but wait on an open blocker.
	Blocked []tracker.Task
	// Human tasks are open and meant for a human, so never handed to an agent.
	Human []tracker.Task
	// Unready tasks are open but meant for neither an agent nor a human yet.
	Unready []tracker.Task
	// Skipped tasks are available but were left open by the agent on every
	// attempt the run allowed, so the run hands them out no more.
	Skipped []tracker.Task
	// Closed tasks are done.
	Closed []tracker.Task
}

// Survey sorts spec's tasks by where they stand.
func Survey(spec tracker.Spec) Remaining {
	var r Remaining
	for _, task := range spec.Tasks {
		switch {
		case !task.Open:
			r.Closed = append(r.Closed, task)
		case task.For == tracker.Human:
			r.Human = append(r.Human, task)
		case task.For != tracker.Agent:
			r.Unready = append(r.Unready, task)
		case blocked(task):
			r.Blocked = append(r.Blocked, task)
		default:
			r.Available = append(r.Available, task)
		}
	}
	return r
}

// Skip moves the available tasks numbered in skipped to Skipped, leaving the
// other kinds alone: a skipped task that is now blocked or closed stays so.
func (r Remaining) Skip(skipped map[int]bool) Remaining {
	available := r.Available
	r.Available, r.Skipped = nil, slices.Clone(r.Skipped)
	for _, task := range available {
		if skipped[task.Number] {
			r.Skipped = append(r.Skipped, task)
		} else {
			r.Available = append(r.Available, task)
		}
	}
	return r
}

// Next returns the first available task, and false when there is none.
func (r Remaining) Next() (tracker.Task, bool) {
	if len(r.Available) == 0 {
		return tracker.Task{}, false
	}
	return r.Available[0], true
}

// Complete reports whether every task of the spec is closed.
func (r Remaining) Complete() bool {
	return len(r.Available)+len(r.Blocked)+len(r.Human)+len(r.Unready)+len(r.Skipped) == 0
}

// blocked reports whether any of task's blockers is still open.
func blocked(task tracker.Task) bool {
	return slices.ContainsFunc(task.Blockers, func(b tracker.Blocker) bool { return b.Open })
}
