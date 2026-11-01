package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/workbench"
)

// readingPreparer records what the file at path held when it prepared a worktree.
type readingPreparer struct {
	name string
	read []string
}

func (r *readingPreparer) Prepare(_ context.Context, dir string, _ io.Writer) error {
	data, err := os.ReadFile(filepath.Join(dir, r.name))
	if err != nil {
		r.read = append(r.read, "absent")
		return nil
	}
	r.read = append(r.read, string(data))
	return nil
}

// openingAt is a bench whose every task worktree is the directory dir.
type openingAt struct {
	loop.Bench
	dir string
}

func (o openingAt) Open(context.Context, int) (string, error) { return o.dir, nil }

// placingLoop is loop wiring on a box whose api repo declares a placement at .env, run from a git worktree of api whose .gitignore holds ignore.
func placingLoop(t *testing.T, ignore string) (loopWiring, *readingPreparer, *fakeAgent) {
	t.Helper()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "api", "worktrees", "work")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	staged := staging.RepoPlacementPathIn(root, "api", ".env")
	if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("TOKEN=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	agent, preparer := &fakeAgent{gh: gh}, &readingPreparer{name: ".env"}
	bench := func(ctx context.Context, _ string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		fake, lag, err := fakeBench(ctx, dir, req)
		return openingAt{Bench: fake, dir: dir}, lag, err
	}
	return loopWiring{
		gh: gh, launcher: agent, lookPath: onPath, preparer: preparer, bench: bench,
		box: resolvedBoxPlacing(workspace, "api", ".env"), root: root,
		git: connection.System(), workdir: func() (string, error) { return dir, nil },
	}, preparer, agent
}

func TestLoopRunOnABoxPlacesTheReposFilesBeforePreparation(t *testing.T) {
	w, preparer, agent := placingLoop(t, ".env\n")

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(preparer.read) != 1 || preparer.read[0] != "TOKEN=1\n" || len(agent.handed) != 1 {
		t.Errorf("exit %d, prepare read %q, handed %v (stderr %q); want 0, the placed bytes and one agent run", code, preparer.read, agent.handed, stderr)
	}
}

func TestLoopRunOnABoxRefusesAPlacementThatIsNotGitignored(t *testing.T) {
	w, preparer, agent := placingLoop(t, "node_modules\n")

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || len(agent.handed) != 0 || len(preparer.read) != 0 || !strings.Contains(stderr, ".env") || !strings.Contains(stderr, ".gitignore") {
		t.Errorf("exit %d, handed %v, prepared %d (stderr %q); want non-zero, no agent run and .env named with .gitignore", code, agent.handed, len(preparer.read), stderr)
	}
}
