package loop_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// hostnamed is a hostname lookup that answers name.
func hostnamed(name string) func() (string, error) {
	return func() (string, error) { return name, nil }
}

// skipLoop is a loop over b on bench, on the machine box-1, that skips #43 if left open.
func skipLoop(t *testing.T, b *board, bench trees) loop.Loop {
	t.Helper()
	l := loopOn(t, b, b)
	l.Bench = bench
	l.Hostname = hostnamed("box-1")
	return l
}

func TestRunCommentsOnASkippedTaskWhereItsWorkWasLeft(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ahead    int
		progress string
	}{
		{"nothing committed", 0, "- **Progress:** nothing committed beyond the work branch `smith/42-spec`"},
		{"one commit", 1, "- **Progress:** 1 commit beyond the work branch `smith/42-spec`"},
		{"several commits", 3, "- **Progress:** 3 commits beyond the work branch `smith/42-spec`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}}

			if _, err := skipLoop(t, b, trees{ahead: tt.ahead}).Run(t.Context(), 42); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			want := []string{"smith skipped this task and handed it to a human: it was still open after its last attempt.\n\n" +
				"- **Machine:** box-1\n" +
				"- **Task worktree:** `/bench/43`\n" +
				"- **Task branch:** `smith/tasks/smith/42-spec/43`\n" +
				"- **Preset:** default\n" +
				"- **Attempts:** 2 in this run\n" +
				tt.progress + "\n"}
			if got := commentsOn(b, 43); !reflect.DeepEqual(got, want) {
				t.Errorf("Run() commented %q on #43, want %q", got, want)
			}
		})
	}
}

func TestRunCommentsOnNoTaskItCloses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		retry bool
	}{
		{"closed on its first attempt", false},
		{"closed on a retry", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &board{tasks: []tracker.Task{agentTask(43)}}
			l := skipLoop(t, b, trees{})
			if tt.retry {
				l.Launcher = &wip{board: b}
			}

			outcome, err := l.Run(t.Context(), 42)

			if err != nil || !outcome.Complete() || len(commentsOn(b, 43)) != 0 {
				t.Errorf("Run() = %+v, %v and commented %q on #43; want the spec complete and no comment on #43", outcome, err, commentsOn(b, 43))
			}
		})
	}
}

func TestRunInteractiveCommentsOnNothing(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := skipLoop(t, b, trees{})
	l.Limits.MaxAttempts = 1

	if _, err := l.RunInteractive(t.Context(), 42); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if len(b.comments) != 0 {
		t.Errorf("RunInteractive() commented %+v, want no comment", b.comments)
	}
}

func TestRunWarnsOfAFailedCommentAndWorksTheNextTask(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}, leaveOpen: []int{43}, refuseComment: errors.New("HTTP 403")}
	var progress strings.Builder
	l := skipLoop(t, b, trees{})
	l.Progress = &progress
	l.Limits.MaxAttempts = 1

	_, err := l.Run(t.Context(), 42)

	want := []string{"smith: warning: could not comment on #43: HTTP 403"}
	if handed := b.handed(); err != nil || !reflect.DeepEqual(handed, []string{"43", "44"}) || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %v, handed %v with progress\n%s\nwant #43 then #44 handed and the lines %q", err, handed, progress.String(), want)
	}
}

func TestRunCommentsOnASkippedTaskMarkingWhatItCouldNotRead(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := skipLoop(t, b, trees{aheadErr: errors.New("git rev-list: exit status 128")})
	l.Hostname = func() (string, error) { return "", errors.New("no hostname") }
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"smith skipped this task and handed it to a human: it was still open after its last attempt.\n\n" +
		"- **Machine:** unknown\n" +
		"- **Task worktree:** `/bench/43`\n" +
		"- **Task branch:** `smith/tasks/smith/42-spec/43`\n" +
		"- **Preset:** default\n" +
		"- **Attempts:** 1 in this run\n" +
		"- **Progress:** unknown\n"}
	if got := commentsOn(b, 43); !reflect.DeepEqual(got, want) {
		t.Errorf("Run() commented %q on #43, want %q", got, want)
	}
}

func TestRunWarnsOfWhatItCouldNotReadForASkippedTasksComment(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	var progress strings.Builder
	l := skipLoop(t, b, trees{aheadErr: errors.New("git rev-list: exit status 128")})
	l.Hostname = func() (string, error) { return "", errors.New("no hostname") }
	l.Progress = &progress
	l.Limits.MaxAttempts = 1

	_, err := l.Run(t.Context(), 42)

	want := []string{
		"smith: warning: could not read this machine's hostname: no hostname",
		"smith: warning: could not count the commits on #43's task branch: git rev-list: exit status 128",
	}
	if err != nil || !containsLines(progress.String(), want) {
		t.Errorf("Run() = %v with progress\n%s\nwant the lines %q", err, progress.String(), want)
	}
}

func TestRunCommentsOnASkippedTaskWhoseAgentWasKilled(t *testing.T) {
	t.Parallel()
	b := &board{tasks: []tracker.Task{agentTask(43)}, leaveOpen: []int{43}}
	l := skipLoop(t, b, trees{})
	l.Launcher = killing{board: b}
	l.Limits.MaxAttempts = 1

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"smith skipped this task and handed it to a human: it was still open after its last attempt.\n\n" +
		"- **Machine:** box-1\n" +
		"- **Task worktree:** `/bench/43`\n" +
		"- **Task branch:** `smith/tasks/smith/42-spec/43`\n" +
		"- **Preset:** default\n" +
		"- **Attempts:** 1 in this run\n" +
		"- **Last attempt:** the agent was killed (likely out of memory)\n" +
		"- **Progress:** nothing committed beyond the work branch `smith/42-spec`\n"}
	if got := commentsOn(b, 43); !reflect.DeepEqual(got, want) {
		t.Errorf("Run() commented %q on #43, want %q", got, want)
	}
}
