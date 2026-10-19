package session_test

import (
	"fmt"
	"path/filepath"
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
