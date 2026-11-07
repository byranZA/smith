package stack_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/byranZA/smith/internal/stack"
)

// call is one command the fake ran: its step, directory and command.
type call struct{ step, dir, command string }

// fakeCommander stands in for the process boundary the stack runs its
// commands through. It records each call and the context it got.
type fakeCommander struct {
	calls []call
	// live holds, per call, whether its context was uncancelled and its
	// deadline, as the call saw them.
	live  []bool
	until []time.Duration
	fail  map[string]error
}

func (f *fakeCommander) Exec(ctx context.Context, dir, step, command string, _ io.Writer) error {
	f.calls = append(f.calls, call{step, dir, command})
	f.live = append(f.live, ctx.Err() == nil)
	deadline, _ := ctx.Deadline()
	f.until = append(f.until, time.Until(deadline))
	return f.fail[step]
}

// worktree is a directory whose repo file holds content.
func worktree(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".smith"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const declared = "stack:\n  up: make up\n  down: make down\n"

func TestEnsureRunsTheDeclaredUpInTheWorktree(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}

	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatalf("Ensure() err = %v", err)
	}

	if want := []call{{"stack up", dir, "make up"}}; !reflect.DeepEqual(run.calls, want) {
		t.Errorf("Ensure() ran %v, want %v", run.calls, want)
	}
}

func TestEnsureRunsNothingForARepoFileWithNoStack(t *testing.T) {
	t.Parallel()
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}

	if err := s.Ensure(t.Context(), worktree(t, "push: true\n"), io.Discard); err != nil {
		t.Fatalf("Ensure() err = %v", err)
	}
	if err := s.Release(t.Context(), io.Discard); err != nil {
		t.Fatalf("Release() err = %v", err)
	}

	if len(run.calls) != 0 {
		t.Errorf("ran %v, want no command", run.calls)
	}
}

func TestEnsureRunsUpAgainOnEachCallWithoutDown(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}

	for range 2 {
		if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
			t.Fatalf("Ensure() err = %v", err)
		}
	}

	want := []call{{"stack up", dir, "make up"}, {"stack up", dir, "make up"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("ran %v, want %v", run.calls, want)
	}
}

func TestEnsureFailsWithTheStepErrorOfAFailedUp(t *testing.T) {
	t.Parallel()
	boom := errors.New("exit status 1")
	run := &fakeCommander{fail: map[string]error{"stack up": boom}}
	s := &stack.Stack{Command: run}

	err := s.Ensure(t.Context(), worktree(t, declared), io.Discard)

	if !errors.Is(err, boom) {
		t.Errorf("Ensure() err = %v, want it to wrap %v", err, boom)
	}
}

func TestReleaseRunsDownOnceInTheWorktreeTheStackCameUpIn(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}
	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := s.Release(t.Context(), io.Discard); err != nil {
			t.Fatalf("Release() err = %v", err)
		}
	}

	want := []call{{"stack up", dir, "make up"}, {"stack down", dir, "make down"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("ran %v, want %v", run.calls, want)
	}
}

func TestReleaseRunsDownAfterTheRunIsCancelledWithinFiveMinutes(t *testing.T) {
	t.Parallel()
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}
	if err := s.Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.Release(ctx, io.Discard); err != nil {
		t.Fatalf("Release() err = %v", err)
	}

	last := len(run.calls) - 1
	if !run.live[last] || run.until[last] <= 4*time.Minute || run.until[last] > 5*time.Minute {
		t.Errorf("down saw a live context = %v with %v left, want live and just under 5 minutes", run.live[last], run.until[last])
	}
}

func TestReleaseRunsTheDownRecordedForTheUpThatRan(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}
	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte("stack:\n  up: other up\n  down: other down\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Release(t.Context(), io.Discard); err != nil {
		t.Fatal(err)
	}

	if got, want := run.calls[1], (call{"stack down", dir, "make down"}); got != want {
		t.Errorf("down ran %v, want %v", got, want)
	}
}

func TestEnsureTakesTheStackDownInTheOldWorktreeBeforeUpInANewOne(t *testing.T) {
	t.Parallel()
	first, second := worktree(t, declared), worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}
	for _, dir := range []string{first, second} {
		if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
			t.Fatal(err)
		}
	}

	want := []call{{"stack up", first, "make up"}, {"stack down", first, "make down"}, {"stack up", second, "make up"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("ran %v, want %v", run.calls, want)
	}
}

func TestEnsureRunsTheRecordedDownBeforeTheNewUpWhenTheCommandsChange(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := &stack.Stack{Command: run}
	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte("stack:\n  up: other up\n  down: other down\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	want := []call{{"stack up", dir, "make up"}, {"stack down", dir, "make down"}, {"stack up", dir, "other up"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("ran %v, want %v", run.calls, want)
	}
}

func TestEnsureOfARepoFileThatRemovedTheStackTakesItDownAndReleasesTheLease(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	dir := worktree(t, declared)
	run := &fakeCommander{}
	s := held(run, state, "repo", "x")
	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".smith", "repo.yaml"), []byte("push: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	want := []call{{"stack up", dir, "make up"}, {"stack down", dir, "make down"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("ran %v, want %v", run.calls, want)
	}
	if holder, err := stack.Holder(state, "repo"); err != nil || holder != nil {
		t.Errorf("Holder() = %v, %v, want the lease released", holder, err)
	}
}

func TestReleaseReportsAFailedDownAndForgetsTheStack(t *testing.T) {
	t.Parallel()
	boom := errors.New("exit status 2")
	run := &fakeCommander{fail: map[string]error{"stack down": boom}}
	s := &stack.Stack{Command: run}
	if err := s.Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}

	err := s.Release(t.Context(), io.Discard)
	again := s.Release(t.Context(), io.Discard)

	if !errors.Is(err, boom) || again != nil || len(run.calls) != 2 {
		t.Errorf("Release() = %v then %v after %d commands, want the failure once then nil", err, again, len(run.calls))
	}
}

// hangingDown is a Commander whose down never exits until its context ends.
type hangingDown struct{}

func (hangingDown) Exec(ctx context.Context, _, step, _ string, _ io.Writer) error {
	if step != "stack down" {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestReleaseStopsAHungDownAtItsTimeoutAndSaysSo(t *testing.T) {
	t.Parallel()
	dir := worktree(t, declared)
	synctest.Test(t, func(t *testing.T) {
		s := &stack.Stack{Command: hangingDown{}}
		if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
			t.Fatal(err)
		}
		start := time.Now()

		err := s.Release(t.Context(), io.Discard)

		want := "take down the stack in " + dir + ": timed out after 5m0s"
		if err == nil || !strings.HasPrefix(err.Error(), want) || time.Since(start) != 5*time.Minute {
			t.Errorf("Release() = %v after %v, want %q after 5m", err, time.Since(start), want)
		}
	})
}
