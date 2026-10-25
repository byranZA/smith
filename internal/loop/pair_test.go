package loop_test

import (
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/tracker"
)

func TestRunInteractiveMarksANamedHumanTaskWhileTheAttachedAgentRuns(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{humanTask(80)}, single: true}

	if _, err := loopOn(t, b, b).RunInteractive(t.Context(), 80); err != nil {
		t.Fatalf("RunInteractive(80) error = %v", err)
	}

	if want := []string{"start 80", "launch 80", "stop 80"}; !slices.Equal(b.events, want) || !b.launched[0].Attached {
		t.Errorf("RunInteractive(80) made %q, launched %+v; want %q with the agent attached", b.events, b.launched, want)
	}
}

func TestRunInteractiveLandsAndPushesANamedHumanTaskTheAgentCloses(t *testing.T) {
	t.Parallel()
	j := &journal{board: &board{tasks: []tracker.Task{humanTask(80)}, single: true}}

	outcome, err := journalLoop(t, j).RunInteractive(t.Context(), 80)

	want := []string{"open 80", "hand 80 in /bench/80", "land 80", "push smith/42-spec", "finish"}
	if err != nil || !outcome.Succeeded() || !slices.Equal(j.events, want) {
		t.Errorf("RunInteractive(80) = %+v, %v, events %q; want it succeeded with events %q", outcome, err, j.events, want)
	}
}

func TestRunInteractiveLeavesANamedHumanTaskLeftOpenToTheHuman(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{humanTask(80)}, single: true, leaveOpen: []int{80}}
	l := loopOn(t, b, b)
	l.Limits.MaxAttempts = 1

	outcome, err := l.RunInteractive(t.Context(), 80)

	if want := []string{"start 80", "launch 80", "stop 80"}; err != nil || outcome.Succeeded() || !slices.Equal(b.events, want) || len(b.comments) != 0 {
		t.Errorf("RunInteractive(80) = %+v, %v, made %q, commented %+v; want it open after %q and no comment", outcome, err, b.events, b.comments, want)
	}
}
