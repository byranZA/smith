// Package tracker reads specs and tasks from where they live, so the loop
// can decide which task is next. v1 knows one tracker, GitHub Issues,
// reached through the gh CLI and gh's own authentication.
package tracker

import (
	"fmt"
	"strings"
)

// Execution is what the loop works for one issue: the issue its work branch
// is named after, and the tasks to work, in order. For a spec that is the spec
// and its tasks; for a task with a parent, its immediate parent and the task
// alone; for a standalone task, the task and itself alone. Single marks an
// execution of one task named on its own, rather than a spec's tasks.
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

// Task is one child of a spec: whether it is open, who it is meant for, and
// the issues blocking it.
type Task struct {
	Number   int
	Title    string
	Open     bool
	For      Audience
	Blockers []Blocker
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
