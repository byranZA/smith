// Package tracker reads specs and their tasks from where they live, so the
// loop can decide which task is next. v1 knows one tracker, GitHub Issues,
// reached through the gh CLI and gh's own authentication.
package tracker

import (
	"fmt"
	"strings"
)

// Spec is a tracker item the loop works: a spec and its tasks, in the order
// the spec lists them.
type Spec struct {
	Number int
	Title  string
	Tasks  []Task
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

// Blocker is an issue blocking a task, and whether it is still open.
type Blocker struct {
	Number int
	Open   bool
}

// NotFoundError reports an issue the tracker has no record of.
type NotFoundError struct {
	Number int
}

// Error implements error.
func (e *NotFoundError) Error() string { return fmt.Sprintf("issue #%d not found", e.Number) }

// NotSpecError reports an issue that exists but is not a spec.
type NotSpecError struct {
	Number int
}

// Error implements error.
func (e *NotSpecError) Error() string {
	return fmt.Sprintf("#%d is not a spec: it is not labelled %q", e.Number, specLabel)
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
