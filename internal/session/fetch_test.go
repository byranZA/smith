package session_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/workbench"
)

// together runs each of fns at the same time and returns their errors in order.
func together(fns ...func() error) []error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	return errs
}

// teammate returns a push to remote's main of a new commit for each round, the
// way a teammate moves the base on.
func teammate(t *testing.T, remote string) func(round int) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "clone", remote, dir)
	return func(round int) {
		commit(t, dir, fmt.Sprintf("round-%d", round))
		git(t, dir, "push", "origin", "main")
	}
}

// sourceOf is a SourceOf of the base on env's repo "smith" that answers only
// with whether it failed.
func sourceOf(t *testing.T, env session.Env) func() error {
	return func() error {
		_, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith"})
		return err
	}
}

func TestSourceOfsStartedTogetherAllSucceed(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)

	push := teammate(t, remote)

	for round := range 5 {
		push(round)

		errs := together(sourceOf(t, env), sourceOf(t, env), sourceOf(t, env), sourceOf(t, env),
			sourceOf(t, env), sourceOf(t, env), sourceOf(t, env), sourceOf(t, env))

		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: SourceOf() %d err = %v", round, i, err)
			}
		}
	}
}

func TestSourceOfAndALoopSetupStartedTogetherBothSucceed(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	trees := make([]string, 4)
	for i := range trees {
		trees[i] = filepath.Join(t.TempDir(), "loop")
		git(t, bare, "worktree", "add", "--detach", trees[i], "main")
	}
	moving := t.TempDir()
	git(t, moving, "clone", remote, moving)

	for round := range 5 {
		var commits []string
		for i := range 4 {
			commit(t, moving, fmt.Sprintf("round-%d-%d", round, i))
			commits = append(commits, gitOut(t, moving, "rev-parse", "HEAD"))
		}
		moveOrigin := func() error {
			for _, c := range commits {
				if err := gitFails(t, moving, "push", "origin", c+":refs/heads/main"); err != nil {
					return fmt.Errorf("push %s: %w", c, err)
				}
			}
			return nil
		}
		setup := func(i int) func() error {
			return func() error {
				req := workbench.Request{Number: 10*round + i + 1, Title: "loop"}
				_, err := workbench.Setup(t.Context(), connection.System(), trees[i], req)
				return err
			}
		}

		errs := together(moveOrigin, sourceOf(t, env), setup(0), setup(1), sourceOf(t, env), setup(2), setup(3))

		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: fetch %d err = %v", round, i, err)
			}
		}
	}
}

// sessionFetchBetween runs git for real, and after each fetch runs a SourceOf on env's repo "smith".
type sessionFetchBetween struct {
	env session.Env
	err *error
}

func (f sessionFetchBetween) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := connection.System().Run(ctx, name, args, stdin, stdout, stderr); err != nil || !slices.Contains(args, "fetch") {
		return err
	}
	_, *f.err = session.SourceOf(ctx, f.env, session.StartRequest{Repo: "smith"})
	return nil
}

func TestALoopSetupInTheBareCloneCutsAtItsBaseWhenASessionFetchesBeforeItReadsTheTip(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	moving := t.TempDir()
	git(t, moving, "clone", remote, moving)
	git(t, moving, "push", "origin", "HEAD:refs/heads/release-2")
	pushToOrigin(t, remote, "main")
	tip := gitOut(t, moving, "rev-parse", "HEAD")
	var sourceErr error

	req := workbench.Request{Number: 1, Title: "loop", Base: "release-2"}
	bench, err := workbench.Setup(t.Context(), sessionFetchBetween{env: env, err: &sourceErr}, bare, req)

	if err != nil || sourceErr != nil {
		t.Fatalf("Setup() err = %v, SourceOf() err = %v; want nil, nil", err, sourceErr)
	}
	if got := gitOut(t, bare, "rev-parse", "refs/heads/"+bench.WorkBranch()); got != tip {
		t.Errorf("%s = %s, want origin's release-2 %s", bench.WorkBranch(), got, tip)
	}
}
