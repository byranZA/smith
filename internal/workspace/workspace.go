// Package workspace converges a box to the workspace its blueprint declares:
// credentials in place, packages installed, runtimes pinned, and every declared
// repo cloned.
//
// It runs on the box, as on-box smith, because that is where the work is
// (docs/adr/0008): it installs packages and clones repos onto the machine it is
// running on, so its commands are local ones and never travel over ssh. The ssh
// boundary sits above it, in the relay that hands `workspace converge` to the
// box.
//
// The package is split into the seams the stage is built from. Plan derives the
// ordered units of work from the staged blueprint and touches nothing — it
// reads no file, runs no command and reaches no box. Converge applies that plan
// through a narrow runner, streaming each unit's progress as it finishes rather
// than banking it all for the end. Result is what the run did, rendered as the
// final summary.
//
// Nothing here parses a blueprint or resolves a placement's source reference:
// the caller loads what `machine setup` staged, and every `from:` in a staged
// document is dead laptop provenance.
package workspace

import (
	"context"
	"io"
)

// Step is one of the four kinds of work the stage does, in the order it does
// them.
type Step string

const (
	// Placements materializes the box-scoped placements from their staged
	// bytes. It is first because it is what puts the box's git identity and
	// forge credentials on disk: a clone attempted before them fails as an
	// authentication error that reads like a credential bug rather than the
	// ordering bug it is.
	Placements Step = "placements"
	// Packages installs the blueprint's apt packages — the OS substrate and
	// build prerequisites, unversioned by design.
	Packages Step = "packages"
	// Toolchain installs mise and writes the generated config pinning the
	// blueprint's tools and exporting its env. It precedes the clones because
	// a token exported through that config is what authenticates one.
	Toolchain Step = "toolchain"
	// Repos clones every declared repo bare into the workspace, under the env
	// the toolchain step put in scope.
	Repos Step = "repos"
)

// Order is the sequence every plan follows. It is the ordering rule itself, in
// one place: a step is converged before another because it puts something on
// the box the later one needs, never for tidiness.
func Order() []Step {
	return []Step{Placements, Packages, Toolchain, Repos}
}

// Unit is one unit of work the stage converges: which step it belongs to and
// what the blueprint declares for it.
type Unit struct {
	// Step is the kind of work this unit is, which decides both when it runs
	// and how it converges.
	Step Step
	// Packages are the apt packages a packages unit installs.
	Packages []string
}

// Runner launches a command on the box smith is running on. It is the system
// boundary the stage reaches apt through, injected so a test drives the real
// converge without an apt to install into.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Env is what a converge run acts through: the commands it drives the box
// with. It is a struct rather than a bare runner because the steps that follow
// this slice add the roots and readers they need beside it.
type Env struct {
	// Command launches the commands the stage converges with — apt today,
	// mise and git as the later steps land.
	Command Runner
}
