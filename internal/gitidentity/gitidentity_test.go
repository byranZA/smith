package gitidentity_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/gitidentity"
)

// fakeGit answers `git config --global --includes --get <key>` from a map of global
// values, exiting 1 for a key that is unset the way git does. A non-nil
// launch error stands in for git not being there to run at all.
type fakeGit struct {
	global map[string]string
	launch error
}

func (f fakeGit) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if f.launch != nil {
		return f.launch
	}
	if name != "git" || len(args) != 5 || !slices.Equal(args[:4], []string{"config", "--global", "--includes", "--get"}) {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	value, ok := f.global[args[4]]
	if !ok {
		return errors.New("exit status 1")
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

	if got := gitidentity.Value(context.Background(), git, "user.name"); got != " Ada Lovelace " {
		t.Errorf("Value() = %q, want the value exactly as git holds it", got)
	}
	if got := gitidentity.Global(context.Background(), git).UserName; got != "Ada Lovelace" {
		t.Errorf("Global().UserName = %q, want it trimmed for the starter", got)
	}
}
