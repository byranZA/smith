package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/bootstrap"
)

// Converge applies the plan to the box smith is running on and reports what
// each step did.
//
// It converges what it can: a step that fails is reported and the ones after
// it still run, because a box whose clone of one repo is unreachable is still
// worth installing the others onto. Every failure travels out in the result —
// the caller exits non-zero on it — rather than the first one aborting the run.
//
// Progress is streamed rather than banked: each step reports on progress as it
// finishes, and the commands it drives write their own output there live, so a
// stage waiting out a busy apt lock does not look hung.
//
// The error it returns is not a step's failure — those are in the result — but
// the progress writer's own, which is the one thing a converge run cannot
// report through.
func Converge(ctx context.Context, env Env, plan []Unit, progress io.Writer) (Result, error) {
	var result Result
	for _, unit := range plan {
		outcome := converge(ctx, env, unit, progress)
		result.Outcomes = append(result.Outcomes, outcome)
		if _, err := io.WriteString(progress, outcome.line()); err != nil {
			return result, fmt.Errorf("write %s progress: %w", unit.Step, err)
		}
	}
	return result, nil
}

// converge applies one unit of work and reports what it did. A step that has
// no converge yet reports itself as such rather than silently passing, so a
// plan can never claim work the stage did not do.
func converge(ctx context.Context, env Env, unit Unit, progress io.Writer) Outcome {
	var summary string
	var err error
	switch unit.Step {
	case Placements:
		summary, err = materialize(env.StateRoot, unit.Placement)
	case Packages:
		summary, err = installPackages(ctx, env.Command, unit.Packages, progress)
	case Toolchain:
		summary, err = convergeToolchain(ctx, env, unit.Fragment, progress)
	case Repos:
		summary, err = convergeRepo(ctx, env.Command, unit.Repo, progress)
	case Orphans:
		summary, err = reportOrphans(unit.Workspace)
	default:
		err = fmt.Errorf("the %s step is not built yet", unit.Step)
	}
	return Outcome{Step: unit.Step, Summary: summary, Err: err}
}

// installPackages installs the declared packages that the box does not already
// hold, and reports what it did.
//
// Only the missing ones reach apt, so an already-satisfied list is answered
// without running apt at all — the no-op a re-run of a converged box is. The
// install carries the base layer's own first-boot lock wait, so a run racing
// cloud-init's unattended-upgrades waits it out rather than aborting at the
// highest-friction moment.
func installPackages(ctx context.Context, run Runner, packages []string, progress io.Writer) (string, error) {
	missing := missingPackages(ctx, run, packages)
	if len(missing) == 0 {
		return "nothing to install; " + strings.Join(packages, ", ") + " already installed", nil
	}
	args := append([]string{"env", "DEBIAN_FRONTEND=noninteractive", "apt-get"}, bootstrap.AptLockArgs()...)
	args = append(args, "install", "-y")
	args = append(args, missing...)
	if err := run.Run(ctx, "sudo", args, nil, progress, progress); err != nil {
		return "", fmt.Errorf("install %s with apt: %w", strings.Join(missing, ", "), err)
	}
	return "installed " + strings.Join(missing, ", "), nil
}

// missingPackages narrows the declared packages to the ones dpkg does not hold
// installed, keeping the order the blueprint declared them in.
func missingPackages(ctx context.Context, run Runner, packages []string) []string {
	var missing []string
	for _, pkg := range packages {
		if !installed(ctx, run, pkg) {
			missing = append(missing, pkg)
		}
	}
	return missing
}

// installed reports whether dpkg holds the named package installed.
//
// A package dpkg has never heard of is one to install, and dpkg-query says so
// by exiting non-zero, so a failed probe is read as the answer rather than as
// an error. The cost of being wrong is bounded: a box where the probe itself
// is broken installs a package apt then reports on, loudly.
func installed(ctx context.Context, run Runner, pkg string) bool {
	var out bytes.Buffer
	args := []string{"-W", "-f", "${db:Status-Status}", pkg}
	if err := run.Run(ctx, "dpkg-query", args, nil, &out, io.Discard); err != nil {
		return false
	}
	return strings.TrimSpace(out.String()) == "installed"
}
