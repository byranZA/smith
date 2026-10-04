package gitidentity_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/gitidentity"
)

// fakeGit answers `git config --global --includes --get <key>` from a map of global
// values, exiting 1 for a key that is unset the way git does. A non-nil
// launch error stands in for git not being there to run at all, and a non-nil
// fail error for git failing with stderr as its diagnostic.
type fakeGit struct {
	global map[string]string
	launch error
	fail   error
	stderr string
}

// exitStatus is a process error carrying the status git exited with.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e exitStatus) ExitCode() int { return int(e) }

func (f fakeGit) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	if f.launch != nil {
		return f.launch
	}
	if f.fail != nil {
		if _, err := io.WriteString(stderr, f.stderr); err != nil {
			return err
		}
		return f.fail
	}
	if name != "git" || len(args) != 5 || !slices.Equal(args[:4], []string{"config", "--global", "--includes", "--get"}) {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	value, ok := f.global[args[4]]
	if !ok {
		return exitStatus(1)
	}
	_, err := fmt.Fprintln(stdout, value)
	return err
}

func TestGlobalReturnsWhicheverValuesAreSet(t *testing.T) {
	for name, tc := range map[string]struct {
		global map[string]string
		want   blueprint.Git
	}{
		"both":       {map[string]string{"user.name": "Ada Lovelace", "user.email": "ada@example.com"}, blueprint.Git{UserName: "Ada Lovelace", UserEmail: "ada@example.com"}},
		"name only":  {map[string]string{"user.name": "Ada Lovelace"}, blueprint.Git{UserName: "Ada Lovelace"}},
		"email only": {map[string]string{"user.email": "ada@example.com"}, blueprint.Git{UserEmail: "ada@example.com"}},
		"neither":    {map[string]string{}, blueprint.Git{}},
	} {
		t.Run(name, func(t *testing.T) {
			got := gitidentity.Global(context.Background(), fakeGit{global: tc.global})

			if got != tc.want {
				t.Errorf("Global() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGlobalIsEmptyWithoutGit(t *testing.T) {
	got := gitidentity.Global(context.Background(), fakeGit{launch: exec.ErrNotFound})

	if got != (blueprint.Git{}) {
		t.Errorf("Global() = %+v, want no identity when git is not installed", got)
	}
}

func TestValueIsExactWhereGlobalTrims(t *testing.T) {
	git := fakeGit{global: map[string]string{"user.name": " Ada Lovelace "}}

	if got, err := gitidentity.Value(context.Background(), git, "user.name"); err != nil || got != " Ada Lovelace " {
		t.Errorf("Value() = %q, %v, want the value exactly as git holds it", got, err)
	}
	if got := gitidentity.Global(context.Background(), git).UserName; got != "Ada Lovelace" {
		t.Errorf("Global().UserName = %q, want it trimmed for the starter", got)
	}
}

func TestValueIsEmptyForAnUnsetKey(t *testing.T) {
	got, err := gitidentity.Value(context.Background(), fakeGit{global: map[string]string{}}, "user.name")

	if err != nil || got != "" {
		t.Errorf("Value() = %q, %v, want an unset key to read as empty without error", got, err)
	}
}

func TestValueReportsAnUnexpectedFailure(t *testing.T) {
	git := fakeGit{fail: exitStatus(128), stderr: "fatal: bad config line 3 in file /home/smith/.gitconfig\n"}

	_, err := gitidentity.Value(context.Background(), git, "user.email")

	if err == nil {
		t.Fatal("Value() error = nil, want the failed read reported")
	}
	for _, want := range []string{"user.email", "bad config line 3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Value() error = %q, want it to contain %q", err, want)
		}
	}
	var exit exitStatus
	if !errors.As(err, &exit) || exit != 128 {
		t.Errorf("Value() error = %v, want it to wrap git's exit status", err)
	}
}

func TestValueReportsGitMissing(t *testing.T) {
	_, err := gitidentity.Value(context.Background(), fakeGit{launch: exec.ErrNotFound}, "user.name")

	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("Value() error = %v, want %v", err, exec.ErrNotFound)
	}
}

func TestValueReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := gitidentity.Value(ctx, fakeGit{fail: exitStatus(1)}, "user.name")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Value() error = %v, want %v", err, context.Canceled)
	}
}
