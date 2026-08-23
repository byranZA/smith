package workspace

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/blueprint"
)

// apiURL is the remote of a second declared repo, so a test can watch one repo
// fail while the other converges.
const apiURL = "https://forge.test/api.git"

// declaresRepos is a blueprint declaring two repos cloned from the forge.
func declaresRepos() blueprint.Blueprint {
	return blueprint.Blueprint{Repos: []blueprint.Repo{
		{Name: "acme", URL: forge},
		{Name: "api", URL: apiURL},
	}}
}

// failures are the units of a run that failed, which is what a test asks a
// result for when the point is that every failure travelled out rather than
// the first one aborting the run.
func failures(r Result) []Outcome {
	var failed []Outcome
	for _, o := range r.Outcomes {
		if o.Err != nil {
			failed = append(failed, o)
		}
	}
	return failed
}

// TestConvergeContinuesPastAFailingRepo proves the stage is per-unit rather
// than all-or-nothing: a repo that cannot be reached is reported and the
// healthy one beside it is still cloned. There is nothing to roll back to by
// the time a clone fails, and refusing everything would deny the reachable
// repos their workspace.
func TestConvergeContinuesPastAFailingRepo(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	box.unreachable[forge] = true

	result, progress := convergeOnBox(t, box, home, declaresRepos())

	if !result.Failed() {
		t.Fatalf("Result.Failed() = false, want the unreachable repo reported: %s", result.Report())
	}
	if want := "git clone --bare " + apiURL; !box.ran(want) {
		t.Errorf("the stage ran %v, want it to clone the reachable repo anyway", box.calls)
	}
	if failed := failures(result); len(failed) != 1 {
		t.Fatalf("%d units failed, want 1: %s", len(failed), result.Report())
	}
	if !strings.Contains(progress, forge) {
		t.Errorf("progress = %q, want the failure named by the repo it was on", progress)
	}
}

// TestConvergeReportsEveryFailure proves the operator is owed the whole
// picture: two unreachable repos fail twice, and the final summary counts both
// rather than stopping at the first.
func TestConvergeReportsEveryFailure(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	box.unreachable[forge] = true
	box.unreachable[apiURL] = true

	result, _ := convergeOnBox(t, box, home, declaresRepos())

	if failed := failures(result); len(failed) != 2 {
		t.Fatalf("%d units failed, want 2: %s", len(failed), result.Report())
	}
	report := result.Report()
	if !strings.Contains(report, "2 failed") {
		t.Errorf("Report() = %q, want it to count both failures", report)
	}
	for _, url := range []string{forge, apiURL} {
		if !strings.Contains(report, url) {
			t.Errorf("Report() = %q, want the failure on %s restated in the summary", report, url)
		}
	}
}

// TestConvergeRerunMutatesNothing proves the whole-stage idempotency property:
// a box already converged to its blueprint is left byte for byte as it was —
// no file rewritten, none removed, none added — and the run exits zero.
func TestConvergeRerunMutatesNothing(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	stage(t, root, "~/.npmrc", "//registry.test/:_authToken=t\n")
	box := newBox()
	box.installed["jq"] = true
	b := declaresRepo("acme")
	b.Placements = []blueprint.Placement{{From: "file:/home/op/npmrc", To: "~/.npmrc", Mode: "converge"}}
	b.Packages = []string{"jq"}
	b.Tools = map[string]string{"node": "20"}
	b.Env = map[string]string{"GITHUB_TOKEN": "env:GITHUB_TOKEN"}

	if result, _ := convergeIn(t, box, root, home, b); result.Failed() {
		t.Fatalf("the first run failed, want a converged box: %s", result.Report())
	}
	then := time.Now().Add(-time.Hour)
	before := aged(t, home, then)
	if len(before) == 0 {
		t.Fatalf("the first run left nothing on the box, want a converged workspace to re-run against")
	}

	result, _ := convergeIn(t, box, root, home, b)

	if result.Failed() {
		t.Fatalf("Result.Failed() = true on a converged re-run, want false: %s", result.Report())
	}
	after := onDisk(t, home)
	for path, mtime := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("the re-run added %s, want it to mutate nothing", path)
			continue
		}
		if !mtime.Equal(then) {
			t.Errorf("the re-run rewrote %s, want it left as it was", path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("the re-run removed %s, want the stage to delete nothing", path)
		}
	}
}

// TestConvergeDeletesNothingUnderTheWorkspaceRoot proves the stage destroys
// nothing it finds there, orphaned or not: a dropped repo, an unpushed
// worktree beside it, and a directory of the operator's own all survive a run
// that no longer declares any of them.
func TestConvergeDeletesNothingUnderTheWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	box := newBox()
	box.hasMise = true
	dropped := clonePath(home, "api")
	alreadyCloned(t, box, dropped, apiURL)
	work := filepath.Join(home, "workspace", "api", "worktrees", "feature", "notes.md")
	if err := os.MkdirAll(filepath.Dir(work), 0o700); err != nil {
		t.Fatalf("put a worktree beside the dropped repo: %v", err)
	}
	if err := os.WriteFile(work, []byte("unpushed\n"), 0o600); err != nil {
		t.Fatalf("put unpushed work in the worktree: %v", err)
	}

	convergeOnBox(t, box, home, declaresRepo("acme"))

	for _, path := range []string{dropped, work} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s is gone after the stage ran: %v", path, err)
		}
	}
}

// aged sets every file under dir to one modification time and returns what it
// found, so a re-run that rewrites a file moves a time the test can name.
func aged(t *testing.T, dir string, when time.Time) map[string]time.Time {
	t.Helper()
	found := onDisk(t, dir)
	for path := range found {
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("age %s: %v", path, err)
		}
		found[path] = when
	}
	return found
}

// onDisk is every file under dir with the time it was last written, which is
// what a converged re-run is held against.
func onDisk(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	found := map[string]time.Time{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		found[path] = info.ModTime()
		return nil
	})
	if err != nil {
		t.Fatalf("read what the box holds under %s: %v", dir, err)
	}
	return found
}

// convergeIn runs the stage over what a blueprint declares, against a box, a
// box state directory and a home of the test's own, and returns what it did
// and what it streamed.
func convergeIn(t *testing.T, box *fakeBox, root, home string, b blueprint.Blueprint) (Result, string) {
	t.Helper()
	stageEnv(t, root, b)
	var progress bytes.Buffer
	env := Env{Command: box, StateRoot: root}
	result, err := Converge(context.Background(), env, Plan(b, home), &progress)
	if err != nil {
		t.Fatalf("Converge() error = %v, want nil", err)
	}
	return result, progress.String()
}
