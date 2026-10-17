package loop_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// recorder fakes where a run records how it ended, failing each record with refuse.
type recorder struct {
	endings []loop.Ending
	reports []string
	refuse  error
}

func (r *recorder) Record(ending loop.Ending, report string) error {
	r.endings = append(r.endings, ending)
	r.reports = append(r.reports, report)
	return r.refuse
}

// commentsOn is each comment b holds on issue number, in order.
func commentsOn(b *board, number int) []string {
	var bodies []string
	for _, c := range b.comments {
		if c.number == number {
			bodies = append(bodies, c.body)
		}
	}
	return bodies
}

func TestRunEndsWithOneOutcomeForEachWayItStops(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		board  board
		setup  func(l *loop.Loop, b *board, cancel context.CancelFunc)
		ending loop.Ending
	}{
		{"every task closed", board{tasks: []tracker.Task{agentTask(43)}}, nil, loop.Complete},
		{"only tasks for a human left", board{tasks: []tracker.Task{agentTask(43), humanTask(44)}}, nil, loop.WaitingOnHuman},
		{"the cap reached", board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}, func(l *loop.Loop, _ *board, _ context.CancelFunc) {
			l.Limits.MaxIterations = 1
		}, loop.CapReached},
		{"tasks skipped until none was available", board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}, nil, loop.AttemptsExhausted},
		{"interrupted", board{tasks: []tracker.Task{agentTask(43)}}, func(l *loop.Loop, b *board, cancel context.CancelFunc) {
			l.Launcher = failing{board: b, cancel: cancel}
		}, loop.Stopped},
		{"stopped on an error", board{tasks: []tracker.Task{agentTask(43)}}, func(l *loop.Loop, b *board, _ context.CancelFunc) {
			j := &journal{board: b, pushErr: errors.New("rejected")}
			l.Bench, l.Pusher = j, j
		}, loop.Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &tt.board
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := &recorder{}
			l := loopOn(t, b, b)
			l.Recorder = r
			if tt.setup != nil {
				tt.setup(&l, b, cancel)
			}

			_, err := l.Run(ctx, 42)

			if want := []loop.Ending{tt.ending}; !reflect.DeepEqual(r.endings, want) || len(commentsOn(b, 42)) != 1 {
				t.Errorf("Run() = %v, recorded %q and commented %q on #42; want %q recorded and one comment", err, r.endings, commentsOn(b, 42), want)
			}
		})
	}
}

func TestRunPostsItsStopReportOnTheSpec(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44), humanTask(45)}}
	r := &recorder{}
	l := loopOn(t, b, b)
	l.Limits.MaxIterations = 1
	l.Recorder = r

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	report := "spec #42 stopped: reached the cap of 1 agent runs\n" +
		"  available: #44\n" +
		"  waiting on a human: #45\n" +
		"work branch: smith/42-spec\n"
	want := []string{"smith stopped its run of this spec: **cap reached**.\n\n```text\n" + report + "```\n"}
	if got := commentsOn(b, 42); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(r.reports, []string{report}) {
		t.Errorf("Run() commented %q and recorded %q, want %q and %q", got, r.reports, want, report)
	}
}

func TestRunOfASingleTaskPostsItsStopReportOnTheTask(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(80)}, single: true}

	if _, err := loopOn(t, b, b).Run(t.Context(), 80); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []posted{{number: 80, body: "smith stopped its run of this task: **complete**.\n\n```text\ntask #80 complete\nwork branch: smith/42-spec\n```\n"}}
	if !reflect.DeepEqual(b.comments, want) {
		t.Errorf("Run() commented %+v, want %+v", b.comments, want)
	}
}

func TestAnInterruptedRunPostsItsStopReportNamingTheInterruption(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if _, err := loopOn(t, b, failing{board: b, cancel: cancel}).Run(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}

	want := []string{"smith stopped its run of this spec: **stopped**.\n\n```text\nspec #42 stopped: interrupted\nwork branch: smith/42-spec\n```\n"}
	if got := commentsOn(b, 42); !reflect.DeepEqual(got, want) {
		t.Errorf("Run() commented %q, want %q", got, want)
	}
}

func TestAFailedRunPostsItsStopReportNamingTheError(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	j := &journal{board: b, pushErr: errors.New("rejected")}

	if _, err := journalLoop(t, j).Run(t.Context(), 42); err == nil {
		t.Fatal("Run() error = nil, want the failed push")
	}

	want := []string{"smith stopped its run of this spec: **failed**.\n\n```text\nspec #42 failed: push the work on #43: rejected\nwork branch: smith/42-spec\n```\n"}
	if got := commentsOn(b, 42); !reflect.DeepEqual(got, want) {
		t.Errorf("Run() commented %q, want %q", got, want)
	}
}

func TestAFailedStopReportIsAWarningThatLeavesTheOutcome(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, refuseComment: errors.New("HTTP 403")}
	r := &recorder{}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Recorder = r
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not post the stop report on #42: HTTP 403"}
	if err != nil || !outcome.Complete() || !reflect.DeepEqual(r.endings, []loop.Ending{loop.Complete}) || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v, recorded %q with progress\n%s\nwant the spec complete, %q recorded and the lines %q", outcome, err, r.endings, progress.String(), loop.Complete, want)
	}
}

func TestAFailedRecordIsAWarning(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	var progress strings.Builder
	l := loopOn(t, b, b)
	l.Recorder = &recorder{refuse: errors.New("read-only file system")}
	l.Progress = &progress

	outcome, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not record how the run ended: read-only file system"}
	if err != nil || !outcome.Complete() || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %+v, %v with progress\n%s\nwant the spec complete and the lines %q", outcome, err, progress.String(), want)
	}
}
