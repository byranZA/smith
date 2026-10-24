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

// op is one operation a test runs alongside others, named for its failure message.
type op struct {
	name string
	run  func() error
}

// together runs each of ops at the same time and fails the test for each that errs.
func together(t *testing.T, round int, ops ...op) {
	t.Helper()
	errs := make([]error, len(ops))
	var wg sync.WaitGroup
	for i, o := range ops {
		wg.Go(func() { errs[i] = o.run() })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("round %d: %s err = %v, want nil", round, ops[i].name, err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
}

// teammate returns a func that pushes a new commit to remote's main for each round.
func teammate(t *testing.T, remote string) func(round int) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "clone", remote, dir)
	return func(round int) {
		commit(t, dir, fmt.Sprintf("round-%d", round))
		git(t, dir, "push", "origin", "main")
	}
}

// sourceOf returns the op that runs SourceOf for the base of env's repo "smith".
func sourceOf(t *testing.T, env session.Env) op {
	t.Helper()
	return op{name: `SourceOf(repo "smith")`, run: func() error {
		_, err := session.SourceOf(t.Context(), env, session.StartRequest{Repo: "smith"})
		return err
	}}
}

func TestSourceOfsStartedTogetherAllSucceed(t *testing.T) {
	t.Parallel()
	env, remote := sourceEnv(t)

	push := teammate(t, remote)

	for round := range 5 {
		push(round)

		together(t, round, sourceOf(t, env), sourceOf(t, env), sourceOf(t, env), sourceOf(t, env),
			sourceOf(t, env), sourceOf(t, env), sourceOf(t, env), sourceOf(t, env))
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
		moveOrigin := op{name: "push to origin's main", run: func() error {
			for _, c := range commits {
				if err := gitFails(t, moving, "push", "origin", c+":refs/heads/main"); err != nil {
					return fmt.Errorf("push %s: %w", c, err)
				}
			}
			return nil
		}}
		setup := func(i int) op {
			req := workbench.Request{Number: 10*round + i + 1, Title: "loop"}
			return op{name: fmt.Sprintf("Setup(#%d in %s)", req.Number, trees[i]), run: func() error {
				_, err := workbench.Setup(t.Context(), connection.System(), trees[i], t.TempDir(), req)
				return err
			}}
		}

		together(t, round, moveOrigin, sourceOf(t, env), setup(0), setup(1), sourceOf(t, env), setup(2), setup(3))
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
	bench, err := workbench.Setup(t.Context(), sessionFetchBetween{env: env, err: &sourceErr}, bare, t.TempDir(), req)

	if err != nil || sourceErr != nil {
		t.Fatalf("Setup(#1 on release-2 in %s) err = %v, SourceOf() between its fetch and tip read err = %v; want nil, nil", bare, err, sourceErr)
	}
	if got := gitOut(t, bare, "rev-parse", "refs/heads/"+bench.WorkBranch()); got != tip {
		t.Errorf("%s = %s, want origin's release-2 %s", bench.WorkBranch(), got, tip)
	}
}
