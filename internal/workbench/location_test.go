package workbench_test

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/workbench"
)

func TestOpenInANonBareRepoGivesTheTaskAWorktreeInTheStateHomeOutsideTheCheckout(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	state := t.TempDir()
	bench, err := workbench.Setup(t.Context(), connection.System(), clone, state, spec430)
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	dir, err := bench.Open(t.Context(), 431)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	want := regexp.MustCompile("^" + regexp.QuoteMeta(filepath.Join(resolved(t, state), "worktrees")) + "/clone-[0-9a-f]{8}/smith/430-loop-works-in-task-worktrees/431$")
	if !want.MatchString(dir) || slices.Contains(strings.Split(dir, string(filepath.Separator)), ".git") || strings.HasPrefix(dir, clone+string(filepath.Separator)) {
		t.Errorf("Open(431) worktree = %q, want one matching %s, with no .git segment, outside the checkout %s", dir, want, clone)
	}
}

// besideOpened returns where bench puts task's new worktree, beside the one Open gives task 1.
func besideOpened(t *testing.T, bench workbench.Workbench, task int) string {
	t.Helper()
	dir, err := bench.Open(t.Context(), 1)
	if err != nil {
		t.Fatalf("Open(1) error = %v", err)
	}
	return filepath.Join(filepath.Dir(dir), strconv.Itoa(task))
}

func TestOpenRefusesAWorktreeOnAnotherBranchAtTheTaskWorktree(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	path := besideOpened(t, bench, 431)
	git(t, clone, "worktree", "add", "-b", "elsewhere", path)

	got, err := bench.Open(t.Context(), 431)

	if err == nil || got != "" || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "elsewhere") || git(t, path, "symbolic-ref", "--short", "HEAD") != "elsewhere" {
		t.Errorf("Open(431) over a worktree on elsewhere = %q, %v; want an error naming %s and elsewhere, with the worktree kept on elsewhere", got, err, path)
	}
}

func TestOpenRefusesAndKeepsAnUnregisteredPathAtTheTaskWorktree(t *testing.T) {
	t.Parallel()
	for name, place := range map[string]func(path string) error{
		"directory": func(path string) error { return os.MkdirAll(path, 0o755) },
		"regular file": func(path string) error {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte("mine\n"), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clone, _ := cloned(t)
			bench := setup(t, clone)
			path := besideOpened(t, bench, 431)
			if err := place(path); err != nil {
				t.Fatal(err)
			}

			got, err := bench.Open(t.Context(), 431)

			_, statErr := os.Stat(path)
			if err == nil || got != "" || !strings.Contains(err.Error(), path) || statErr != nil {
				t.Errorf("Open(431) over an unregistered %s = %q, %v, with stat %v; want an error naming %s and the %s kept", name, got, err, statErr, path, name)
			}
		})
	}
}

func TestOpenKeepsTheCauseOfAFilesystemErrorAtTheTaskWorktree(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	if err := os.MkdirAll(stateOf(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateOf(clone), "worktrees"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := bench.Open(t.Context(), 431)

	if !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("Open(431) under a regular file error = %v, want one wrapping %v", err, syscall.ENOTDIR)
	}
}

func TestOpenGivesTwoClonesOfARepoNamedAlikeTheirOwnStateHomeDirectories(t *testing.T) {
	t.Parallel()
	_, remote := cloned(t)
	state := t.TempDir()
	var dirs []string
	for range 2 {
		clone := filepath.Join(t.TempDir(), "clone")
		git(t, filepath.Dir(clone), "clone", remote, clone)
		bench, err := workbench.Setup(t.Context(), connection.System(), clone, state, spec430)
		if err != nil {
			t.Fatalf("Setup(%s) error = %v", clone, err)
		}
		dir, err := bench.Open(t.Context(), 431)
		if err != nil {
			t.Fatalf("Open(431) in %s error = %v", clone, err)
		}
		dirs = append(dirs, dir)
	}

	repoDir := func(dir string) string {
		return strings.SplitN(strings.TrimPrefix(dir, resolved(t, state)), string(filepath.Separator), 4)[2]
	}
	if repoDir(dirs[0]) == repoDir(dirs[1]) {
		t.Errorf("Open(431) worktrees of two clones named clone = %q, want them under different <repo>-<hash> directories", dirs)
	}
}

func TestOpenInABareRepoOrItsLinkedWorktreeGivesTheTaskAWorktreeUnderTheGitCommonDir(t *testing.T) {
	t.Parallel()
	for name, dirOf := range map[string]func(t *testing.T, bare string) string{
		"bare repo": func(*testing.T, string) string { return "" },
		"linked worktree of a bare repo": func(t *testing.T, bare string) string {
			tree := filepath.Join(t.TempDir(), "session")
			git(t, bare, "worktree", "add", "--detach", tree, "main")
			return tree
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, remote := cloned(t)
			bare := filepath.Join(t.TempDir(), "repo.git")
			git(t, filepath.Dir(bare), "clone", "--bare", remote, bare)
			dir := cmp.Or(dirOf(t, bare), bare)
			bench, err := workbench.Setup(t.Context(), connection.System(), dir, t.TempDir(), spec430)
			if err != nil {
				t.Fatalf("Setup(%s) error = %v", dir, err)
			}

			got, err := bench.Open(t.Context(), 431)

			want := filepath.Join(resolved(t, bare), "smith", "worktrees", "smith", "430-loop-works-in-task-worktrees", "431")
			if err != nil || got != want {
				t.Errorf("Open(431) in %s = %q, %v, want %q, nil", dir, got, err, want)
			}
		})
	}
}

func TestOpenFindsAFailedTasksWorktreeLeftUnderTheGitDirByItsTaskBranch(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	bench := setup(t, clone)
	old := filepath.Join(resolved(t, filepath.Join(clone, ".git")), "smith", "worktrees", "smith", "430-loop-works-in-task-worktrees", "431")
	git(t, clone, "worktree", "add", "-b", bench.TaskBranch(431), old, bench.WorkBranch())

	got, err := bench.Open(t.Context(), 431)

	if err != nil || got != old {
		t.Errorf("Open(431) with its task branch checked out at %s = %q, %v, want %q, nil", old, got, err, old)
	}
}
