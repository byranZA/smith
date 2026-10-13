package workbench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/workbench"
)

// git runs git with args in dir, failing the test on error, and returns its trimmed output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=smith", "-c", "user.email=smith@example.com"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// cloned returns a clone of a fresh bare remote whose default branch main holds one commit, and the remote.
func cloned(t *testing.T) (clone, remote string) {
	t.Helper()
	root := t.TempDir()
	remote, seed, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "seed"), filepath.Join(root, "clone")
	git(t, root, "init", "--bare", "-b", "main", remote)
	git(t, root, "clone", remote, seed)
	git(t, seed, "commit", "--allow-empty", "-m", "seed")
	git(t, seed, "push", "origin", "main")
	git(t, root, "clone", remote, clone)
	return clone, remote
}

// spec430 is the request for spec #430, whose work branch is smith/430-loop-works-in-task-worktrees.
var spec430 = workbench.Request{Number: 430, Title: "Spec: Loop works in task worktrees"}

func setup(t *testing.T, clone string) workbench.Workbench {
	t.Helper()
	bench, err := workbench.Setup(t.Context(), connection.System(), clone, spec430)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	return bench
}

// commitIn commits a file named name in dir and returns the commit.
func commitIn(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", name)
	git(t, dir, "commit", "-m", name)
	return git(t, dir, "rev-parse", "HEAD")
}

func TestSetupCutsANewWorkBranchFromOriginsDefaultBranch(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	later := filepath.Join(t.TempDir(), "later")
	git(t, filepath.Dir(later), "clone", remote, later)
	git(t, later, "commit", "--allow-empty", "-m", "since the clone")
	git(t, later, "push", "origin", "main")

	bench := setup(t, clone)

	want := "smith/430-loop-works-in-task-worktrees"
	if got := bench.WorkBranch(); got != want {
		t.Fatalf("WorkBranch() = %q, want %q", got, want)
	}
	if got, tip := git(t, clone, "rev-parse", "refs/heads/"+want), git(t, remote, "rev-parse", "main"); got != tip {
		t.Errorf("%s = %s, want origin's main %s", want, got, tip)
	}
}

func TestSetupCutsANewWorkBranchFromTheNamedBase(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	later := filepath.Join(t.TempDir(), "later")
	git(t, filepath.Dir(later), "clone", remote, later)
	git(t, later, "commit", "--allow-empty", "-m", "on release-2")
	git(t, later, "push", "origin", "HEAD:refs/heads/release-2")
	req := spec430
	req.Base = "release-2"

	bench, err := workbench.Setup(t.Context(), connection.System(), clone, req)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got, tip := git(t, clone, "rev-parse", "refs/heads/"+bench.WorkBranch()), git(t, remote, "rev-parse", "release-2"); got != tip {
		t.Errorf("%s = %s, want origin's release-2 %s", bench.WorkBranch(), got, tip)
	}
}

func TestSetupCutsFromTheNamedBaseWhenOriginNamesNoDefaultBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	remote, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	git(t, root, "init", "--bare", "-b", "main", remote)
	git(t, root, "clone", remote, clone)
	git(t, clone, "commit", "--allow-empty", "-m", "on release-2")
	git(t, clone, "push", "origin", "HEAD:refs/heads/release-2")
	req := spec430
	req.Base = "release-2"

	bench, err := workbench.Setup(t.Context(), connection.System(), clone, req)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	if got, tip := git(t, clone, "rev-parse", "refs/heads/"+bench.WorkBranch()), git(t, remote, "rev-parse", "release-2"); got != tip {
		t.Errorf("%s = %s, want origin's release-2 %s", bench.WorkBranch(), got, tip)
	}
}

func TestSetupKeepsAWorkBranchItAlreadyCut(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	landed := commitIn(t, dir, "431.txt")
	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land() error = %v", err)
	}

	again := setup(t, clone)

	if got := git(t, clone, "rev-parse", "refs/heads/"+again.WorkBranch()); got != landed {
		t.Errorf("work branch after a second Setup() = %s, want the landed %s", got, landed)
	}
}

func TestSetupRefusesWhenOriginNamesNoDefaultBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	remote, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	git(t, root, "init", "--bare", "-b", "main", remote)
	git(t, root, "clone", remote, clone)

	_, err := workbench.Setup(t.Context(), connection.System(), clone, spec430)

	if err == nil || !strings.Contains(err.Error(), "origin names no default branch") || !strings.Contains(err.Error(), "git ls-remote --symref origin HEAD") {
		t.Errorf("Setup() error = %v; want it to name the missing default branch and its fix", err)
	}
}

func TestOpenGivesTheTaskAWorktreeUnderTheGitDirOnABranchFromTheWorkBranchTip(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)

	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	common := resolved(t, git(t, clone, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	got := []string{resolved(t, dir), git(t, dir, "rev-parse", "HEAD"), git(t, dir, "symbolic-ref", "--short", "HEAD")}
	want := []string{filepath.Join(common, "smith", "worktrees", "431"), git(t, clone, "rev-parse", bench.WorkBranch()), "smith/tasks/smith/430-loop-works-in-task-worktrees/431"}
	if !slices.Equal(got, want) {
		t.Errorf("Open(431) worktree, HEAD and branch = %q, want %q", got, want)
	}
}

func TestOpenLeavesTheOperatorsDirtyCheckoutAlone(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	if err := os.WriteFile(filepath.Join(clone, "wip.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := []string{git(t, clone, "symbolic-ref", "HEAD"), git(t, clone, "rev-parse", "HEAD"), git(t, clone, "status", "--porcelain")}
	bench := setup(t, clone)

	if _, err := bench.Open(t.Context(), 431); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	after := []string{git(t, clone, "symbolic-ref", "HEAD"), git(t, clone, "rev-parse", "HEAD"), git(t, clone, "status", "--porcelain")}
	if !slices.Equal(after, before) {
		t.Errorf("operator's checkout branch, HEAD and status = %q, want them unchanged as %q", after, before)
	}
}

func TestOpenAgainReturnsTheSameWorktreeWithItsChanges(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	first, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "half.txt"), []byte("half done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := bench.Open(t.Context(), 431)

	if err != nil || second != first || git(t, second, "status", "--porcelain") != "?? half.txt" {
		t.Errorf("second Open() = %q, %v; want %q still holding half.txt", second, err, first)
	}
}

func TestLandFastForwardsTheWorkBranchToTheTasksCommits(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	commitIn(t, dir, "a.txt")
	last := commitIn(t, dir, "b.txt")

	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land() error = %v", err)
	}

	if got := git(t, clone, "rev-parse", bench.WorkBranch()); got != last {
		t.Errorf("work branch after Land() = %s, want the task's last commit %s", got, last)
	}
}

func TestLandRemovesTheTaskWorktreeAndTaskBranch(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	commitIn(t, dir, "a.txt")

	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land() error = %v", err)
	}

	_, statErr := os.Stat(dir)
	if branches := git(t, clone, "branch", "--list", "smith/tasks/*"); !os.IsNotExist(statErr) || branches != "" {
		t.Errorf("after Land() worktree stat = %v, task branches %q; want the worktree and branch gone", statErr, branches)
	}
}

func TestLandWithNoCommitsLeavesTheWorkBranchWhereItWas(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	before := git(t, clone, "rev-parse", bench.WorkBranch())
	if _, err := bench.Open(t.Context(), 431); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	err := bench.Land(t.Context(), 431)

	if got := git(t, clone, "rev-parse", bench.WorkBranch()); err != nil || got != before {
		t.Errorf("Land() = %v, work branch %s; want no error and the work branch still %s", err, got, before)
	}
}

func TestTheNextTaskStartsFromTheLandedWork(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	first, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open(431) error = %v", err)
	}
	landed := commitIn(t, first, "431.txt")
	if err := bench.Land(t.Context(), 431); err != nil {
		t.Fatalf("Land(431) error = %v", err)
	}

	next, err := bench.Open(t.Context(), 432)
	if err != nil {
		t.Fatalf("Open(432) error = %v", err)
	}

	if got := git(t, next, "rev-parse", "HEAD"); got != landed {
		t.Errorf("#432's task branch starts at %s, want #431's landed %s", got, landed)
	}
}

func TestLandRefusesADirtyTaskWorktree(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	before := git(t, clone, "rev-parse", bench.WorkBranch())
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	commitIn(t, dir, "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "left.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = bench.Land(t.Context(), 431)

	_, statErr := os.Stat(dir)
	if got := git(t, clone, "rev-parse", bench.WorkBranch()); err == nil || !strings.Contains(err.Error(), dir) || got != before || statErr != nil {
		t.Errorf("Land() = %v, work branch %s, worktree stat %v; want an error naming %s, the work branch still %s and the worktree kept", err, got, statErr, dir, before)
	}
}

func TestLandRefusesATaskBranchThatCannotFastForward(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	commitIn(t, dir, "a.txt")
	moved := git(t, clone, "commit-tree", "-p", bench.WorkBranch(), "-m", "elsewhere", bench.WorkBranch()+"^{tree}")
	git(t, clone, "update-ref", "refs/heads/"+bench.WorkBranch(), moved)

	err = bench.Land(t.Context(), 431)

	if got := git(t, clone, "rev-parse", bench.WorkBranch()); err == nil || !strings.Contains(err.Error(), bench.WorkBranch()) || got != moved {
		t.Errorf("Land() = %v, work branch %s; want an error naming %s and the work branch still %s", err, got, bench.WorkBranch(), moved)
	}
}

// resolved returns path with its symlinks evaluated, so a temp dir compares equal however it was reached.
func resolved(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSetupAdoptsALocalWorkBranchByNumberAfterTheSpecIsRenamed(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "switch", "-c", "smith/430-old-title")
	docs := commitIn(t, clone, "docs.md")
	git(t, clone, "switch", "main")

	bench := setup(t, clone)
	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	got := []string{bench.WorkBranch(), git(t, dir, "rev-parse", "HEAD")}
	want := []string{"smith/430-old-title", docs}
	if !slices.Equal(got, want) {
		t.Errorf("work branch and #431's start = %q, want %q", got, want)
	}
}

func TestSetupAdoptsAWorkBranchOnlyOnOrigin(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", remote, other)
	docs := commitIn(t, other, "docs.md")
	git(t, other, "push", "origin", "HEAD:refs/heads/smith/430-old-title")

	bench := setup(t, clone)

	got := []string{bench.WorkBranch(), git(t, clone, "rev-parse", "refs/heads/smith/430-old-title")}
	want := []string{"smith/430-old-title", docs}
	if !slices.Equal(got, want) {
		t.Errorf("work branch and its local tip = %q, want %q", got, want)
	}
}

func TestSetupRefusesAWorkBranchThatHasDivergedFromOrigin(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	git(t, clone, "branch", "smith/430-x", "main")
	git(t, clone, "push", "origin", "smith/430-x")
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "-b", "smith/430-x", remote, other)
	commitIn(t, other, "theirs.txt")
	git(t, other, "push", "origin", "smith/430-x")
	git(t, clone, "switch", "smith/430-x")
	ours := commitIn(t, clone, "ours.txt")
	git(t, clone, "switch", "main")

	_, err := workbench.Setup(t.Context(), connection.System(), clone, spec430)

	if got := git(t, clone, "rev-parse", "refs/heads/smith/430-x"); err == nil || !strings.Contains(err.Error(), "smith/430-x") || got != ours {
		t.Errorf("Setup() = %v, smith/430-x at %s; want an error naming smith/430-x and the branch still %s", err, got, ours)
	}
}

func TestSetupRefusesMoreThanOneBranchMatchingTheNumber(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "branch", "smith/430-a", "main")
	git(t, clone, "push", "origin", "main:refs/heads/smith/430-b")

	_, err := workbench.Setup(t.Context(), connection.System(), clone, spec430)

	if err == nil || !strings.Contains(err.Error(), "smith/430-a") || !strings.Contains(err.Error(), "smith/430-b") {
		t.Errorf("Setup() error = %v; want it to name smith/430-a and smith/430-b", err)
	}
}

func TestSetupIgnoresBranchesOfOtherNumbers(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "branch", "smith/4301-other", "main")
	git(t, clone, "branch", "smith/43-other", "main")

	bench := setup(t, clone)

	if got, want := bench.WorkBranch(), "smith/430-loop-works-in-task-worktrees"; got != want {
		t.Errorf("WorkBranch() = %q, want %q", got, want)
	}
}

func TestSetupAdoptsALocalWorkBranchAheadOfOrigin(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "push", "origin", "main:refs/heads/smith/430-x")
	git(t, clone, "switch", "-c", "smith/430-x", "main")
	ahead := commitIn(t, clone, "ahead.txt")
	git(t, clone, "switch", "main")

	bench := setup(t, clone)

	got := []string{bench.WorkBranch(), git(t, clone, "rev-parse", "refs/heads/smith/430-x")}
	want := []string{"smith/430-x", ahead}
	if !slices.Equal(got, want) {
		t.Errorf("work branch and its tip = %q, want %q", got, want)
	}
}
