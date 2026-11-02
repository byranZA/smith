package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
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

// localPlacingLoop is loop wiring off a box, run from subdir of a real checkout of api with a gitignored .env holding TOKEN=local, whose origin is origin and whose config home holds a blueprint declaring the https://github.com/acme/api repo with a placement at .env.
func localPlacingLoop(t *testing.T, origin, subdir string) (loopWiring, *readingPreparer, *fakeAgent, string) {
	t.Helper()
	checkout := t.TempDir()
	gitIn(t, checkout, "init", "-b", "main")
	gitIn(t, checkout, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(checkout, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".env"), []byte("TOKEN=local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(checkout, subdir)
	if err := os.MkdirAll(run, 0o750); err != nil {
		t.Fatal(err)
	}
	task := t.TempDir()
	gitIn(t, task, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(task, ".gitignore"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "blueprints"), 0o750); err != nil {
		t.Fatal(err)
	}
	doc := "repos:\n  - url: https://github.com/acme/api\n    placements:\n      - from: file:~/x\n        to: .env\n        mode: converge\n"
	if err := os.WriteFile(filepath.Join(home, "blueprints", "acme.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	agent, preparer := &fakeAgent{gh: gh}, &readingPreparer{name: ".env"}
	bench := func(ctx context.Context, _ string, req workbench.Request) (loop.Bench, workbench.Lag, error) {
		fake, lag, err := fakeBench(ctx, task, req)
		return openingAt{Bench: fake, dir: task}, lag, err
	}
	return loopWiring{
		gh: gh, launcher: agent, lookPath: onPath, preparer: preparer, bench: bench,
		git: connection.System(), workdir: func() (string, error) { return run, nil },
		home: func() (config.Home, error) { return config.NewHome(home), nil },
	}, preparer, agent, task
}

func TestLoopRunOffABoxPlacesTheCheckoutsFileBeforePreparation(t *testing.T) {
	w, preparer, agent, _ := localPlacingLoop(t, "git@github.com:Acme/API.git", ".")

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(preparer.read) != 1 || preparer.read[0] != "TOKEN=local\n" || len(agent.handed) != 1 {
		t.Errorf("exit %d, prepare read %q, handed %v (stderr %q); want 0, the checkout's bytes and one agent run", code, preparer.read, agent.handed, stderr)
	}
}

func TestLoopRunOffABoxFromASubdirectoryPlacesFromTheTopLevel(t *testing.T) {
	w, preparer, _, _ := localPlacingLoop(t, "https://github.com/acme/api", "pkg/deep")

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(preparer.read) != 1 || preparer.read[0] != "TOKEN=local\n" {
		t.Errorf("exit %d, prepare read %q (stderr %q); want the top level's bytes", code, preparer.read, stderr)
	}
}

func TestLoopRunOffABoxOfAnUndeclaredRepoPlacesNothing(t *testing.T) {
	w, preparer, _, _ := localPlacingLoop(t, "https://github.com/other/api", ".")

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(preparer.read) != 1 || preparer.read[0] != "absent" || strings.Contains(stderr, "placement") {
		t.Errorf("exit %d, prepare read %q (stderr %q); want nothing placed and no placement output", code, preparer.read, stderr)
	}
}

func TestLoopRunOffABoxRefusesADestinationTheRepoDoesNotIgnore(t *testing.T) {
	w, preparer, agent, task := localPlacingLoop(t, "https://github.com/acme/api", ".")
	if err := os.WriteFile(filepath.Join(task, ".gitignore"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || len(agent.handed) != 0 || len(preparer.read) != 0 || !strings.Contains(stderr, ".gitignore") {
		t.Errorf("exit %d, handed %v, prepared %d (stderr %q); want a refusal naming .gitignore", code, agent.handed, len(preparer.read), stderr)
	}
}

func TestLoopRunOffABoxRefusesASourceMissingFromTheCheckout(t *testing.T) {
	w, preparer, agent, _ := localPlacingLoop(t, "https://github.com/acme/api", ".")
	top, err := w.gitOutput(context.Background(), w.mustWorkdir(t), "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(top, ".env")); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || len(agent.handed) != 0 || len(preparer.read) != 0 || !strings.Contains(stderr, filepath.Join(top, ".env")) || !strings.Contains(stderr, "acme.yaml") {
		t.Errorf("exit %d, handed %v, prepared %d (stderr %q); want a refusal naming the source path, the checkout and the blueprint", code, agent.handed, len(preparer.read), stderr)
	}
}

func TestLoopRunOffABoxRefusesASourceTheCheckoutDoesNotIgnore(t *testing.T) {
	w, preparer, agent, _ := localPlacingLoop(t, "https://github.com/acme/api", ".")
	top, err := w.gitOutput(context.Background(), w.mustWorkdir(t), "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, ".gitignore"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || len(agent.handed) != 0 || len(preparer.read) != 0 || !strings.Contains(stderr, filepath.Join(top, ".env")) || !strings.Contains(stderr, ".gitignore") {
		t.Errorf("exit %d, handed %v, prepared %d (stderr %q); want a refusal naming the source and .gitignore", code, agent.handed, len(preparer.read), stderr)
	}
}

func TestLoopRunOffABoxRefusesATrackedSource(t *testing.T) {
	w, preparer, agent, _ := localPlacingLoop(t, "https://github.com/acme/api", ".")
	dir := w.mustWorkdir(t)
	gitIn(t, dir, "add", "-f", ".env")

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || len(agent.handed) != 0 || len(preparer.read) != 0 || !strings.Contains(stderr, "tracked file the loop will not copy") || !strings.Contains(stderr, ".env") {
		t.Errorf("exit %d, handed %v, prepared %d (stderr %q); want a refusal naming .env as a tracked file the loop will not copy", code, agent.handed, len(preparer.read), stderr)
	}
}

// mustWorkdir returns the directory the wiring runs in.
func (w loopWiring) mustWorkdir(t *testing.T) string {
	t.Helper()
	dir, err := w.workdir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoopRunOffABoxWarnsOfAnUnreadableBlueprintAndPlacesTheRest(t *testing.T) {
	w, preparer, agent, _ := localPlacingLoop(t, "https://github.com/acme/api", ".")
	home, err := w.home()
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(home.Path(), "blueprints", "bad.yaml")
	if err := os.WriteFile(bad, []byte("repos: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || len(preparer.read) != 1 || preparer.read[0] != "TOKEN=local\n" || len(agent.handed) != 1 ||
		!strings.Contains(stderr, "smith: warning: skipping blueprint "+bad) {
		t.Errorf("exit %d, prepare read %q (stderr %q); want the placement made and a warning naming %s", code, preparer.read, stderr, bad)
	}
}
