// Package tracker reads specs and tasks from where they live, so the loop
// can decide which task is next, marks the task an agent is working or the
// loop gave up on, and posts the comment the loop writes on a task it skips. v1 knows one tracker, GitHub Issues, reached through
// the gh CLI and gh's own authentication.
package tracker

import (
	"fmt"
	"strings"
)

// Execution is what the loop works for one issue: Number and Title name the
// issue its work branch is named after, and Tasks the tasks to work, in order.
// Single marks one task named on its own, whose branch is its parent's or its
// own, rather than a spec's tasks.
type Execution struct {
	Number int
	Title  string
	Single bool
	Tasks  []Task
}

// Issue returns the number and title of the issue the execution was read for:
// its one task when Single, and the spec its work branch is named after
// otherwise.
func (e Execution) Issue() (int, string) {
	if e.Single && len(e.Tasks) == 1 {
		return e.Tasks[0].Number, e.Tasks[0].Title
	}
	return e.Number, e.Title
}

// Task is one child of a spec: whether it is open, who it is meant for, the
// issues blocking it, and whether it is marked as worked.
type Task struct {
	Number   int
	Title    string
	Open     bool
	For      Audience
	Blockers []Blocker
	// Working reports whether the task is marked as worked by an agent. It is
	// a signal for people, never a reason to choose a task or pass one over.
	Working bool
	// Presets names each preset the task's preset: labels choose, in label
	// order. Which one it runs on, and whether two is too many, is for the
	// preset choice to decide.
	Presets []string
}

// Audience is who a task is meant for.
type Audience int

// The audiences a task can be meant for. A task meant for Nobody has not been
// made ready for either.
const (
	Nobody Audience = iota
	Agent
	Human
)

// Blocker is an issue blocking a task, and whether it is still open. Repo
// names the owner/name of a blocker outside the spec's repo, and is empty for
// one inside it.
type Blocker struct {
	Repo   string
	Number int
	Open   bool
}

// NotFoundError reports an issue the tracker has no record of.
type NotFoundError struct {
	Number int
}

// Error implements error.
func (e *NotFoundError) Error() string { return fmt.Sprintf("issue #%d not found", e.Number) }

// ForeignError reports an issue in a repo other than the one the loop works,
// which v1 refuses rather than take for the same-numbered issue here.
type ForeignError struct {
	Ref  Ref
	Repo string
}

// Error implements error.
func (e *ForeignError) Error() string {
	return fmt.Sprintf("%s is not in %s: the loop works only issues in the repo it is run in", e.Ref, e.Repo)
}

// UnavailableError reports that the tracker could not be reached at all, with
// what the tool reaching it said.
type UnavailableError struct {
	Tool   string
	Detail string
	Err    error
}

// Error implements error.
func (e *UnavailableError) Error() string {
	if detail := strings.TrimSpace(e.Detail); detail != "" {
		return fmt.Sprintf("%s could not be used: %s", e.Tool, detail)
	}
	return fmt.Sprintf("%s could not be used: %v", e.Tool, e.Err)
}

// Unwrap returns the underlying failure.
func (e *UnavailableError) Unwrap() error { return e.Err }

// AuthError reports that the tracker was reached but refused the credentials
// the tool reaching it holds, with what the tool said.
type AuthError struct {
	Tool   string
	Detail string
	Err    error
}

// Error implements error.
func (e *AuthError) Error() string {
	if detail := strings.TrimSpace(e.Detail); detail != "" {
		return fmt.Sprintf("the tracker can't authenticate with %s: %s", e.Tool, detail)
	}
	return fmt.Sprintf("the tracker can't authenticate with %s: %v", e.Tool, e.Err)
}

// Unwrap returns the underlying failure.
func (e *AuthError) Unwrap() error { return e.Err }

// Labels are the names of the triage labels that say what an issue is for:
// Agent and Human mark who a task is for, and Spec marks a spec not yet
// split into tasks.
type Labels struct {
	Agent string
	Human string
	Spec  string
}

// DefaultLabels returns the triage labels a repo uses unless it names its own.
func DefaultLabels() Labels {
	return Labels{Agent: "ready-for-agent", Human: "ready-for-human", Spec: "spec"}
}
