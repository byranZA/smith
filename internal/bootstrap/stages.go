package bootstrap

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Stage is one admin-side step of the setup pipeline: a name the operator sees
// as the run reaches it, and the work it does on a box whose phases have all
// completed. A stage is not a phase — it runs from the operator's machine after
// bootstrap.sh is done, so it can neither revert nor damage what the phases
// established, and a failed one leaves a provisioned box behind.
type Stage struct {
	// Name is what the stage reports itself as, in the operator's vocabulary.
	Name string
	// Run does the stage's work. Its error stops the pipeline.
	Run func(ctx context.Context) error
}

// StageFailure is a stage of the pipeline that failed: the stage smith stopped
// at, the stages that completed before it, and the error that stage reported.
// Every stage after the failed one is skipped, because each stage builds on
// what the ones before it established.
type StageFailure struct {
	// Stage is the stage that failed.
	Stage string
	// Completed are the stages that succeeded before it, in order.
	Completed []string
	// Err is the failed stage's own error.
	Err error
}

// Error implements error.
func (f *StageFailure) Error() string {
	return fmt.Sprintf("the %q stage failed: %v", f.Stage, f.Err)
}

// Unwrap returns the failed stage's own error, so a caller can inspect what the
// stage reported rather than only which stage it was.
func (f *StageFailure) Unwrap() error { return f.Err }

// Report renders what the operator is told about a failed stage. It reads
// distinctly from a phase failure on purpose: every phase completed, so the box
// is provisioned and secured, and what stopped is a step that runs on top of a
// box that is already there.
func (f *StageFailure) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "✗ setup failed at the %q stage\n\n", f.Stage)
	fmt.Fprintf(&b, "  cause: %v\n\n", f.Err)
	b.WriteString("  every setup phase completed, so the box is provisioned and secured.\n")
	if len(f.Completed) > 0 {
		fmt.Fprintf(&b, "  completed stages: %s\n", strings.Join(f.Completed, ", "))
	} else {
		b.WriteString("  completed stages: none\n")
	}
	b.WriteString("  No stage after the one above ran; fix the cause and re-run setup.\n")
	return b.String()
}

// RunStages runs the pipeline's stages in the order given, reporting each by
// name to out as it begins and again when it completes, and stopping at the
// first failure with a *StageFailure naming that stage and the ones that
// completed before it.
//
// The order is the pipeline's whole content: each stage assumes what the ones
// before it established, so a failure short-circuits the rest rather than
// carrying on to a stage whose ground is missing.
func RunStages(ctx context.Context, stages []Stage, out io.Writer) error {
	var completed []string
	for _, stage := range stages {
		if _, err := fmt.Fprintf(out, "%s%s\n", phaseStartPrefix, stage.Name); err != nil {
			return fmt.Errorf("write stage start: %w", err)
		}
		if err := stage.Run(ctx); err != nil {
			return &StageFailure{Stage: stage.Name, Completed: completed, Err: err}
		}
		if _, err := fmt.Fprintf(out, "%s%s\n", phaseDonePrefix, stage.Name); err != nil {
			return fmt.Errorf("write stage done: %w", err)
		}
		completed = append(completed, stage.Name)
	}
	return nil
}
