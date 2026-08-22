package staging

import (
	"fmt"
	"strings"
)

// Resolver turns a placement's source reference into the value it names. It is
// the seam onto internal/secret's resolver: staging never reads a file or an
// environment variable itself, so every scheme that package learns is a scheme
// a placement can be sourced from, and a test can hand in a resolver of its
// own.
type Resolver func(ref string) (string, error)

// UnresolvedSource is one placement whose source reference smith could not
// resolve on the operator's machine: which placement it was, and why the
// reference did not resolve.
type UnresolvedSource struct {
	// From is the source reference as the blueprint declared it.
	From string
	// Repo is the repo whose worktree the file would have landed in, empty for
	// a box-scoped placement. It is carried so two repos declaring the same
	// destination are told apart in the refusal.
	Repo string
	// Destination is where the file would have landed, as the blueprint
	// declared it.
	Destination string
	// Err is why the reference did not resolve.
	Err error
}

// UnresolvedError refuses a staging run whose placement sources do not all
// resolve. It carries every failure rather than the first, because the operator
// fixes a blueprint full of typos in one pass rather than one run per typo.
type UnresolvedError struct {
	// Sources are the placements whose references did not resolve, in the
	// order the blueprint declares them.
	Sources []UnresolvedSource
}

// Error names every unresolvable reference and the placement it belongs to, one
// per line, and says plainly that the box was left untouched.
func (e *UnresolvedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d placement source(s) will not resolve, so nothing was staged:", len(e.Sources))
	for _, s := range e.Sources {
		fmt.Fprintf(&b, "\n  %s from %s: %v", s.scope(), s.From, s.Err)
	}
	return b.String()
}

// Unwrap exposes the underlying resolution failures, so errors.Is reaches
// through the refusal to the reason any one reference did not resolve.
func (e *UnresolvedError) Unwrap() []error {
	errs := make([]error, 0, len(e.Sources))
	for _, s := range e.Sources {
		errs = append(errs, s.Err)
	}
	return errs
}

// scope names the placement as the blueprint declares it: its destination,
// qualified by the repo when it is repo-scoped.
func (s UnresolvedSource) scope() string {
	if s.Repo == "" {
		return s.Destination
	}
	return s.Repo + " " + s.Destination
}

// Resolve attaches each placement's bytes to the planned tree, returning a tree
// of its own rather than filling in the one it was given.
//
// Every source resolves on the operator's machine and nowhere else:
// file:/home/op/.secrets/npmrc means nothing on a VPS, so the bytes are what
// travel and the reference stays behind as provenance.
//
// It is resolve-all-then-write: every reference is tried before Converge writes
// a byte, and one that does not resolve refuses the whole run with the box
// untouched. A half-staged box fails later, inside a session, which is the
// worst place to find out. Every failure is reported, not just the first, so
// the operator fixes them in one pass.
func Resolve(tree Tree, resolve Resolver) (Tree, error) {
	placements := make([]Placement, len(tree.Placements))
	copy(placements, tree.Placements)
	var unresolved []UnresolvedSource
	for i, p := range placements {
		value, err := resolve(p.From)
		if err != nil {
			unresolved = append(unresolved, UnresolvedSource{From: p.From, Repo: p.Repo, Destination: p.Destination, Err: err})
			continue
		}
		placements[i].File.Bytes = []byte(value)
	}
	if len(unresolved) > 0 {
		return Tree{}, &UnresolvedError{Sources: unresolved}
	}
	tree.Placements = placements
	return tree, nil
}
