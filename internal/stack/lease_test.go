package stack_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/byranZA/smith/internal/stack"
)

// held is a stack on the state directory for the repo key, working branch.
func held(run *fakeCommander, state, key, branch string) *stack.Stack {
	return &stack.Stack{Command: run, State: state, Key: key, Branch: branch}
}

func TestEnsureRefusesAStackAnotherRunHoldsNamingItsBranchAndWorktree(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	first, second := worktree(t, declared), worktree(t, declared)
	holder := held(&fakeCommander{}, state, "repo", "smith/1-a")
	if err := holder.Ensure(t.Context(), first, io.Discard); err != nil {
		t.Fatal(err)
	}
	run := &fakeCommander{}

	err := held(run, state, "repo", "smith/2-b").Ensure(t.Context(), second, io.Discard)
	runtime.KeepAlive(holder)

	var busy *stack.HeldError
	if !errors.As(err, &busy) || busy.Branch != "smith/1-a" || busy.Dir != first {
		t.Fatalf("Ensure() err = %v, want a *HeldError for smith/1-a in %s", err, first)
	}
	if len(run.calls) != 0 {
		t.Errorf("Ensure() ran %v, want no command", run.calls)
	}
	if msg := err.Error(); !strings.Contains(msg, "smith/1-a") || !strings.Contains(msg, first) {
		t.Errorf("Ensure() err = %q, want the holder's branch and worktree", msg)
	}
}

func TestEnsureTakesSeparateLeasesForSeparateRepos(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	one, two := worktree(t, declared), worktree(t, declared)
	if err := held(&fakeCommander{}, state, "repo-a", "x").Ensure(t.Context(), one, io.Discard); err != nil {
		t.Fatal(err)
	}

	if err := held(&fakeCommander{}, state, "repo-b", "y").Ensure(t.Context(), two, io.Discard); err != nil {
		t.Errorf("Ensure() of another repo err = %v, want it to go on", err)
	}
}

func TestEnsureOfARepoFileWithNoStackTakesNoLease(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	if err := held(&fakeCommander{}, state, "repo", "x").Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}

	err := held(&fakeCommander{}, state, "repo", "y").Ensure(t.Context(), worktree(t, "push: true\n"), io.Discard)

	if err != nil {
		t.Errorf("Ensure() err = %v, want a run with no stack to go on beside the holder", err)
	}
}

func TestReleaseFreesTheLeaseAndDeletesItsRecord(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	s := held(&fakeCommander{}, state, "repo", "x")
	if err := s.Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(t.Context(), io.Discard); err != nil {
		t.Fatal(err)
	}

	holder, err := stack.Holder(state, "repo")
	if err != nil || holder != nil {
		t.Errorf("Holder() = %v, %v, want none after Release", holder, err)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 0 {
		t.Errorf("state holds %v after Release, want no record", files)
	}
}

func TestReleaseWhoseDownFailsFreesTheLockAndKeepsTheRecord(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	run := &fakeCommander{fail: map[string]error{"stack down": errors.New("boom")}}
	s := held(run, state, "repo", "x")
	if err := s.Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(t.Context(), io.Discard); err == nil {
		t.Fatal("Release() err = nil, want the failed down")
	}

	if holder, err := stack.Holder(state, "repo"); err != nil || holder != nil {
		t.Errorf("Holder() = %v, %v, want the lock free", holder, err)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 1 {
		t.Errorf("state holds %v, want the record kept", files)
	}
}

func TestHolderReportsTheRunThatHoldsTheLease(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	dir := worktree(t, declared)
	if err := held(&fakeCommander{}, state, "repo", "smith/3-c").Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}

	got, err := stack.Holder(state, "repo")

	if err != nil || got == nil || got.Branch != "smith/3-c" || got.Dir != dir || got.Up != "make up" || got.Down != "make down" {
		t.Errorf("Holder() = %+v, %v, want smith/3-c in %s with its up and down", got, err, dir)
	}
}

func TestKeyIsTheSameForTheSSHAndHTTPSFormsOfAnOrigin(t *testing.T) {
	t.Parallel()
	a := stack.Key("git@github.com:Acme/Api.git", "/x/.git")
	b := stack.Key("https://github.com/acme/api", "/y/.git")
	if a != b {
		t.Errorf("Key() = %q and %q, want one lease for one origin", a, b)
	}
}

func TestKeyFallsBackToTheGitDirectoryWithNoOrigin(t *testing.T) {
	t.Parallel()
	if a, b := stack.Key("", "/x/.git"), stack.Key("", "/y/.git"); a == b || a == "" {
		t.Errorf("Key() = %q and %q, want a key per git directory", a, b)
	}
}

// helperEnv makes a test binary run holdLease instead of the tests.
const helperEnv = "SMITH_STACK_LEASE_HELPER"

func TestMain(m *testing.M) {
	if state := os.Getenv(helperEnv); state != "" {
		s := &stack.Stack{Command: &fakeCommander{}, State: state, Key: "repo", Branch: "child"}
		if err := s.Ensure(context.Background(), os.Args[len(os.Args)-1], io.Discard); err != nil {
			_, _ = os.Stderr.WriteString(err.Error())
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString("held\n")
		select {}
	}
	os.Exit(m.Run())
}

func TestALeaseHolderKilledBySIGKILLLeavesTheLockFreeAndTheRecordInPlace(t *testing.T) {
	// Not parallel: a process forked while another test holds a lease briefly
	// shares its lock until it execs.
	state, dir := t.TempDir(), worktree(t, declared)
	child := exec.Command(os.Args[0], dir)
	child.Env = append(os.Environ(), helperEnv+"="+state)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "held\n" {
		t.Fatalf("child said %q, want it to hold the lease", line)
	}
	if got, err := stack.Holder(state, "repo"); err != nil || got == nil {
		t.Fatalf("Holder() = %v, %v, want the child holding", got, err)
	}
	if err := child.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()

	got, err := stack.Holder(state, "repo")

	if err != nil || got != nil {
		t.Errorf("Holder() = %v, %v, want the lock free after SIGKILL", got, err)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 1 {
		t.Errorf("state holds %v, want the record in place", files)
	}
}

// stale leaves a record in state for repo as a run killed after up would:
// an unlocked lease file holding the worktree and its commands.
func stale(t *testing.T, state string) string {
	t.Helper()
	dir := worktree(t, declared)
	run := &fakeCommander{fail: map[string]error{"stack down": errors.New("boom")}}
	s := held(run, state, "repo", "smith/9-z")
	if err := s.Ensure(t.Context(), dir, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(t.Context(), io.Discard); err == nil {
		t.Fatal("Release() err = nil, want the failed down")
	}
	return dir
}

func TestRecoverRunsTheRecordedDownInTheRecordedWorktreeAndClearsTheRecord(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	dir := stale(t, state)
	run := &fakeCommander{}

	rec, err := held(run, state, "repo", "smith/10-y").Recover(t.Context(), io.Discard)

	if err != nil || rec == nil || rec.Dir != dir || rec.Branch != "smith/9-z" {
		t.Fatalf("Recover() = %+v, %v, want the record of smith/9-z in %s", rec, err, dir)
	}
	if want := []call{{"stack down", dir, "make down"}}; !reflect.DeepEqual(run.calls, want) {
		t.Errorf("Recover() ran %v, want %v", run.calls, want)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 0 {
		t.Errorf("state holds %v, want the record cleared", files)
	}
}

func TestRecoverWithNothingLeftRunsNothing(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	run := &fakeCommander{}

	rec, err := held(run, state, "repo", "x").Recover(t.Context(), io.Discard)

	if err != nil || rec != nil || len(run.calls) != 0 {
		t.Errorf("Recover() = %v, %v after %v, want nothing", rec, err, run.calls)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 0 {
		t.Errorf("state holds %v, want no lease file", files)
	}
}

func TestRecoverNeverTouchesALiveHoldersLease(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	holder := held(&fakeCommander{}, state, "repo", "smith/1-a")
	if err := holder.Ensure(t.Context(), worktree(t, declared), io.Discard); err != nil {
		t.Fatal(err)
	}
	run := &fakeCommander{}

	rec, err := held(run, state, "repo", "smith/2-b").Recover(t.Context(), io.Discard)
	runtime.KeepAlive(holder)

	if err != nil || rec != nil || len(run.calls) != 0 {
		t.Errorf("Recover() = %v, %v after %v, want a live lease left alone", rec, err, run.calls)
	}
	if got, _ := stack.Holder(state, "repo"); got == nil || got.Branch != "smith/1-a" {
		t.Errorf("Holder() = %v, want the live holder's record kept", got)
	}
}

func TestRecoverWhoseDownFailsKeepsTheRecordForTheNextRun(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	dir := stale(t, state)
	run := &fakeCommander{fail: map[string]error{"stack down": errors.New("exit status 3")}}

	rec, err := held(run, state, "repo", "x").Recover(t.Context(), io.Discard)

	if rec == nil || err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("Recover() = %v, %v, want the record and an error naming %s and the status", rec, err, dir)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 1 {
		t.Errorf("state holds %v, want the record kept", files)
	}
	if got, _ := stack.Holder(state, "repo"); got != nil {
		t.Errorf("Holder() = %v, want the lock free", got)
	}
}

func TestRecoverOfAGoneWorktreeNamesItAndTheCommandAndClearsTheRecord(t *testing.T) {
	t.Parallel()
	state := t.TempDir()
	dir := stale(t, state)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	run := &fakeCommander{}

	rec, err := held(run, state, "repo", "x").Recover(t.Context(), io.Discard)

	if rec == nil || err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "make down") {
		t.Fatalf("Recover() = %v, %v, want the record and an error naming %s and make down", rec, err, dir)
	}
	if len(run.calls) != 0 {
		t.Errorf("Recover() ran %v, want no command", run.calls)
	}
	if files, _ := filepath.Glob(filepath.Join(state, "*", "*")); len(files) != 0 {
		t.Errorf("state holds %v, want the record cleared", files)
	}
}
