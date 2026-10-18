package prepare_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/prepare"
)

// exitError is a process that ran and exited with code.
type exitError struct {
	code int
}

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (e exitError) ExitCode() int { return e.code }

// fakeRunner stands in for the processes preparation launches. It records the
// argv of each call, writes say to the call's stdout, and answers with err.
type fakeRunner struct {
	calls [][]string
	say   string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if _, err := io.WriteString(stdout, f.say); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return f.err
}

func TestPrepareInstallsTheToolchainPointedAtTheWorktree(t *testing.T) {
	t.Parallel()
	run := &fakeRunner{}
	p := prepare.Preparer{Command: run, Mise: "/home/smith/.local/bin/mise"}

	if err := p.Prepare(t.Context(), "/work/smith/worktrees/spec-42", io.Discard); err != nil {
		t.Fatalf("Prepare() err = %v", err)
	}

	want := [][]string{{"/home/smith/.local/bin/mise", "--cd", "/work/smith/worktrees/spec-42", "install"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Errorf("Prepare() ran %v, want %v", run.calls, want)
	}
}

func TestPrepareStreamsTheToolchainOutputToTheCaller(t *testing.T) {
	t.Parallel()
	run := &fakeRunner{say: "mise node@22.1.0 ✓ installed\n"}
	p := prepare.Preparer{Command: run, Mise: "mise"}
	var out strings.Builder

	if err := p.Prepare(t.Context(), "/work/smith/worktrees/spec-42", &out); err != nil {
		t.Fatalf("Prepare() err = %v", err)
	}

	if got, want := out.String(), "mise node@22.1.0 ✓ installed\n"; got != want {
		t.Errorf("Prepare() wrote %q, want %q", got, want)
	}
}

func TestPrepareNamesTheToolchainStepAndExitStatusOnFailure(t *testing.T) {
	t.Parallel()
	run := &fakeRunner{err: exitError{code: 3}}
	p := prepare.Preparer{Command: run, Mise: "mise"}

	err := p.Prepare(t.Context(), "/work/smith/worktrees/spec-42", io.Discard)

	var failed *prepare.StepError
	if !errors.As(err, &failed) {
		t.Fatalf("Prepare() err = %v, want a *prepare.StepError", err)
	}
	if failed.Step != prepare.Toolchain || failed.Status != 3 {
		t.Errorf("Prepare() failed step %q with status %d, want step %q with status 3", failed.Step, failed.Status, prepare.Toolchain)
	}
	if want := "the toolchain step exited with status 3"; !strings.Contains(err.Error(), want) {
		t.Errorf("Prepare() err = %q, want it to contain %q", err, want)
	}
}

func TestPrepareNamesTheToolchainStepWhenMiseCannotRun(t *testing.T) {
	t.Parallel()
	run := &fakeRunner{err: errors.New(`exec: "mise": executable file not found in $PATH`)}
	p := prepare.Preparer{Command: run, Mise: "mise"}

	err := p.Prepare(t.Context(), "/work/smith/worktrees/spec-42", io.Discard)

	want := `the toolchain step could not run: mise --cd /work/smith/worktrees/spec-42 install: exec: "mise": executable file not found in $PATH`
	if err == nil || err.Error() != want {
		t.Errorf("Prepare() err = %v, want %q", err, want)
	}
}
