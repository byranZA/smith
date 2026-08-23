package staging

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Resolver turns a reference the blueprint declares into the value it names. It
// is the seam onto the resolvers above staging: staging never reads a file or
// an environment variable itself, so every scheme they learn is a scheme a
// blueprint can name, and a test can hand in a resolver of its own.
//
// There are two of them, because a placement source and a blueprint value do
// not accept the same schemes: a source resolves to a whole file's bytes and so
// refuses literal:, which a value accepts on purpose.
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

// UnresolvedValue is one variable of the blueprint's env whose reference smith
// could not resolve on the operator's machine: which variable it was, and why
// the reference did not resolve.
type UnresolvedValue struct {
	// Ref is the reference as the blueprint declared it.
	Ref string
	// Repo is the repo whose toolchain the variable belongs to, empty for a
	// box-scoped one. It is carried so two repos declaring the same variable
	// are told apart in the refusal.
	Repo string
	// Name is the variable as the blueprint declared it.
	Name string
	// Err is why the reference did not resolve.
	Err error
}

// UnresolvedError refuses a staging run whose references do not all resolve. It
// carries every failure rather than the first, because the operator fixes a
// blueprint full of typos in one pass rather than one run per typo.
type UnresolvedError struct {
	// Sources are the placements whose references did not resolve, in the
	// order the blueprint declares them.
	Sources []UnresolvedSource
	// Values are the env variables whose references did not resolve, by
	// variable name within each scope.
	Values []UnresolvedValue
}

// Error names every unresolvable reference and what declared it, one per line,
// and says plainly that the box was left untouched.
func (e *UnresolvedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d reference(s) will not resolve, so nothing was staged:", len(e.Sources)+len(e.Values))
	for _, s := range e.Sources {
		fmt.Fprintf(&b, "\n  %s from %s: %v", s.scope(), s.From, s.Err)
	}
	for _, v := range e.Values {
		fmt.Fprintf(&b, "\n  %s from %s: %v", v.scope(), v.Ref, v.Err)
	}
	return b.String()
}

// Unwrap exposes the underlying resolution failures, so errors.Is reaches
// through the refusal to the reason any one reference did not resolve.
func (e *UnresolvedError) Unwrap() []error {
	errs := make([]error, 0, len(e.Sources)+len(e.Values))
	for _, s := range e.Sources {
		errs = append(errs, s.Err)
	}
	for _, v := range e.Values {
		errs = append(errs, v.Err)
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

// scope names the variable as the blueprint declares it, qualified by the repo
// when it is repo-scoped.
func (v UnresolvedValue) scope() string {
	if v.Repo == "" {
		return v.Name
	}
	return v.Repo + " " + v.Name
}

// Resolve fills in everything the operator's machine must read before the box
// is touched: each placement's bytes, and the value of every variable the
// blueprint's env declares. It returns a tree of its own rather than filling in
// the one it was given.
//
// Every reference resolves on the operator's machine and nowhere else.
// file:/home/op/.secrets/npmrc means nothing on a VPS and env:GH_TOKEN names a
// variable in the operator's shell, so the values are what travel and the
// references stay behind as provenance. A box that re-resolved them against its
// own environment or filesystem would export something else entirely, or
// nothing.
//
// The two resolvers are the two grammars: source for a placement's from:, which
// resolves to a whole file's bytes, and value for an env entry, which also
// accepts the literal: a source refuses.
//
// It is resolve-all-then-write: every reference is tried before Converge writes
// a byte, and one that does not resolve refuses the whole run with the box
// untouched. A half-staged box fails later, inside a session, which is the
// worst place to find out. Every failure is reported, not just the first, so
// the operator fixes them in one pass.
func Resolve(tree Tree, source, value Resolver) (Tree, error) {
	placements := make([]Placement, len(tree.Placements))
	copy(placements, tree.Placements)
	var unresolved UnresolvedError
	for i, p := range placements {
		resolved, err := source(p.From)
		if err != nil {
			unresolved.Sources = append(unresolved.Sources, UnresolvedSource{From: p.From, Repo: p.Repo, Destination: p.Destination, Err: err})
			continue
		}
		placements[i].File.Bytes = []byte(resolved)
	}
	env, failures := resolveEnv(tree.Env.References, value)
	unresolved.Values = failures
	if len(unresolved.Sources) > 0 || len(unresolved.Values) > 0 {
		return Tree{}, &unresolved
	}
	data, err := env.bytes()
	if err != nil {
		return Tree{}, err
	}
	tree.Placements = placements
	tree.Env.File.Bytes = data
	return tree, nil
}

// resolveEnv turns every reference the blueprint's env declares into the value
// it names, and reports the ones that would not resolve rather than stopping at
// the first.
//
// Variables are resolved in sorted order within each scope, and the scopes in
// the order the blueprint declares them, so a blueprint full of typos is
// reported the same way from one run to the next.
func resolveEnv(references Env, value Resolver) (Env, []UnresolvedValue) {
	var failures []UnresolvedValue
	resolved := Env{}
	scope := func(repo string, declared map[string]string) map[string]string {
		values := make(map[string]string, len(declared))
		for _, name := range slices.Sorted(maps.Keys(declared)) {
			v, err := value(declared[name])
			if err != nil {
				failures = append(failures, UnresolvedValue{Ref: declared[name], Repo: repo, Name: name, Err: err})
				continue
			}
			values[name] = v
		}
		return values
	}
	if len(references.Box) > 0 {
		resolved.Box = scope("", references.Box)
	}
	for _, repo := range slices.Sorted(maps.Keys(references.Repos)) {
		if resolved.Repos == nil {
			resolved.Repos = make(map[string]map[string]string, len(references.Repos))
		}
		resolved.Repos[repo] = scope(repo, references.Repos[repo])
	}
	return resolved, failures
}
