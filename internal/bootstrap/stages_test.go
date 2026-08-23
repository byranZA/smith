package bootstrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// namedStage returns a stage that records that it ran and then answers with
// err, so a test can assert on which stages a run reached.
func namedStage(name string, ran *[]string, err error) Stage {
	return Stage{Name: name, Run: func(context.Context) error {
		*ran = append(*ran, name)
		return err
	}}
}

func TestRunStagesRunsEveryStageInOrder(t *testing.T) {
	var ran []string
	stages := []Stage{
		namedStage("access", &ran, nil),
		namedStage("install", &ran, nil),
	}

	if err := RunStages(context.Background(), stages, io.Discard); err != nil {
		t.Fatalf("RunStages() err = %v, want nil", err)
	}
	if got := strings.Join(ran, ","); got != "access,install" {
		t.Errorf("stages ran = %q, want them in the order they were given", got)
	}
}

func TestRunStagesNamesEachStageInTheOutput(t *testing.T) {
	var ran []string
	var out strings.Builder

	if err := RunStages(context.Background(), []Stage{namedStage("install", &ran, nil)}, &out); err != nil {
		t.Fatalf("RunStages() err = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "install") {
		t.Errorf("output = %q, want the stage to report itself by name", out.String())
	}
}

func TestRunStagesStopsAtTheStageThatFailed(t *testing.T) {
	var ran []string
	stages := []Stage{
		namedStage("access", &ran, nil),
		namedStage("install", &ran, errors.New("no release asset")),
		namedStage("workspace", &ran, nil),
	}

	err := RunStages(context.Background(), stages, io.Discard)

	var failure *StageFailure
	if !errors.As(err, &failure) {
		t.Fatalf("RunStages() err = %v, want a *StageFailure", err)
	}
	if failure.Stage != "install" {
		t.Errorf("failed stage = %q, want the stage that failed named", failure.Stage)
	}
	if got := strings.Join(ran, ","); got != "access,install" {
		t.Errorf("stages ran = %q, want no stage after the failed one to run", got)
	}
}

func TestStageFailureCarriesTheStagesThatCompleted(t *testing.T) {
	var ran []string
	stages := []Stage{
		namedStage("access", &ran, nil),
		namedStage("install", &ran, errors.New("no release asset")),
	}

	var failure *StageFailure
	if !errors.As(RunStages(context.Background(), stages, io.Discard), &failure) {
		t.Fatal("RunStages() err is not a *StageFailure")
	}
	if got := strings.Join(failure.Completed, ","); got != "access" {
		t.Errorf("completed stages = %q, want the stages that ran before the failure", got)
	}
}

func TestStageFailureUnwrapsToTheStagesOwnError(t *testing.T) {
	cause := errors.New("no release asset")
	var ran []string

	err := RunStages(context.Background(), []Stage{namedStage("install", &ran, cause)}, io.Discard)

	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false, want the stage's own error reachable")
	}
}

func TestStageFailureReportSeparatesAStageFromAPhase(t *testing.T) {
	failure := StageFailure{
		Stage:     "install",
		Completed: []string{"access"},
		Err:       errors.New("no release asset"),
	}

	report := failure.Report()

	for _, want := range []string{"install", "stage", "no release asset", "access"} {
		if !strings.Contains(report, want) {
			t.Errorf("Report() = %q, want it to name %q", report, want)
		}
	}
	if !strings.Contains(report, "phase") {
		t.Errorf("Report() = %q, want it to report the phases as complete", report)
	}
}
