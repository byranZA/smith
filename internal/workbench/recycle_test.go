package workbench_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/workbench"
)

func TestTheNextTaskIsCheckedOutInTheLandedTasksWorktreeAtTheWorkBranchTip(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	first, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open(431) error = %v", err)
	}
	commitIn(t, first, "431.txt")
	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land(431) error = %v", err)
	}

	next, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("Open(432) error = %v", err)
	}

	got := []string{next, git(t, next, "symbolic-ref", "--short", "HEAD"), git(t, next, "rev-parse", "HEAD")}
	want := []string{first, "smith/tasks/smith/430-loop-works-in-task-worktrees/432", git(t, clone, "rev-parse", bench.WorkBranch())}
	if !slices.Equal(got, want) {
		t.Errorf("Open(432) worktree, branch and HEAD = %q, want %q", got, want)
	}
}

func TestAnIgnoredDirectoryCarriesOverToTheNextTask(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	first, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open(431) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, ".gitignore"), []byte(".venv/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, first, "add", ".gitignore")
	git(t, first, "commit", "-m", "ignore .venv")
	if err := os.MkdirAll(filepath.Join(first, ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, ".venv", "pyvenv.cfg"), []byte("home = /usr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land(431) error = %v", err)
	}

	next, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("Open(432) error = %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(next, ".venv", "pyvenv.cfg")); err != nil || string(got) != "home = /usr\n" {
		t.Errorf("#432's .venv/pyvenv.cfg = %q, %v; want %q carried over from #431", got, err, "home = /usr\n")
	}
}

func TestARetryOfARecycledTaskRunsInTheSameWorktreeOnTheSameBranch(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	if _, err := bench.Open(t.Context(), 431); err != nil {
		t.Fatalf("Open(431) error = %v", err)
	}
	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land(431) error = %v", err)
	}
	first, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("first Open(432) error = %v", err)
	}
	commitIn(t, first, "half.txt")

	retry, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("second Open(432) error = %v", err)
	}

	got := []string{retry, git(t, retry, "symbolic-ref", "--short", "HEAD"), git(t, retry, "log", "-1", "--format=%s")}
	want := []string{first, "smith/tasks/smith/430-loop-works-in-task-worktrees/432", "half.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("retried Open(432) worktree, branch and last commit = %q, want %q", got, want)
	}
}

func TestTheTaskAfterASkippedTaskGetsANewWorktree(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	skipped, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open(431) error = %v", err)
	}
	stuck := commitIn(t, skipped, "stuck.txt")
	if err := os.WriteFile(filepath.Join(skipped, "half.txt"), []byte("half done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	next, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("Open(432) error = %v", err)
	}

	got := []string{git(t, next, "symbolic-ref", "--short", "HEAD"), git(t, skipped, "symbolic-ref", "--short", "HEAD"), git(t, skipped, "rev-parse", "HEAD"), git(t, skipped, "status", "--porcelain")}
	want := []string{"smith/tasks/smith/430-loop-works-in-task-worktrees/432", "smith/tasks/smith/430-loop-works-in-task-worktrees/431", stuck, "?? half.txt"}
	if next == skipped || !slices.Equal(got, want) {
		t.Errorf("Open(432) = %q beside skipped %q, branches, skipped HEAD and status %q; want a new worktree and %q", next, skipped, got, want)
	}
}

func TestOpenGivesAnotherWorkBranchItsOwnWorktreeLeavingTheFirstsAlone(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	req := spec430
	req.Branch = "work-a"
	benchA, err := workbench.Setup(t.Context(), connection.System(), clone, req)
	if err != nil {
		t.Fatalf("Setup(work-a) error = %v", err)
	}
	dirA, err := benchA.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() on work-a error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "half.txt"), []byte("half done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req.Branch = "work-b"
	benchB, err := workbench.Setup(t.Context(), connection.System(), clone, req)
	if err != nil {
		t.Fatalf("Setup(work-b) error = %v", err)
	}

	dirB, err := benchB.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() on work-b error = %v", err)
	}

	got := []string{git(t, dirB, "symbolic-ref", "--short", "HEAD"), git(t, dirA, "symbolic-ref", "--short", "HEAD"), git(t, dirA, "status", "--porcelain")}
	want := []string{"smith/tasks/work-b/431", "smith/tasks/work-a/431", "?? half.txt"}
	if dirB == dirA || !slices.Equal(got, want) {
		t.Errorf("Open(431) on work-b = %q beside work-a's %q, branches and work-a's status %q; want its own worktree and %q", dirB, dirA, got, want)
	}
}
