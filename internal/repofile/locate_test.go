package repofile_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/repofile"
)

// fakeGit answers `git rev-parse --show-toplevel` with root, exiting 128 when root is empty.
type fakeGit struct {
	root string
}

func (f fakeGit) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name != "git" || len(args) != 4 || args[0] != "-C" || !slices.Equal(args[2:], []string{"rev-parse", "--show-toplevel"}) {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	if f.root == "" {
		return errors.New("exit status 128")
	}
	_, err := fmt.Fprintln(stdout, f.root)
	return err
}

func TestLocateFindsTheRepoRootGitReports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "internal", "cli")
	home := t.TempDir()

	repo, err := repofile.Locate(t.Context(), fakeGit{root: root}, sub, config.NewHome(home))
	if err != nil {
		t.Fatalf("Locate(%q, home %q) error = %v", sub, home, err)
	}

	if want := filepath.Join(root, ".smith", "repo.yaml"); repo.Path() != want {
		t.Errorf("Locate(%q, home %q).Path() = %q, want %q", sub, home, repo.Path(), want)
	}
}

func TestLocateRefusesOutsideAGitRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	home := t.TempDir()

	_, err := repofile.Locate(t.Context(), fakeGit{}, dir, config.NewHome(home))

	if !errors.Is(err, repofile.ErrNotARepo) {
		t.Errorf("Locate(%q, home %q) error = %v, want ErrNotARepo", dir, home, err)
	}
}

func TestLocateRefusesWhenTheReposSmithIsTheConfigHome(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".smith")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home))

	if err == nil || !strings.Contains(err.Error(), home) {
		t.Errorf("Locate(%q, home %q) error = %v, want a refusal naming the config home", root, home, err)
	}
}

func TestLocateRefusesTheConfigHomeReachedThroughASymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(link, ".smith")

	_, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home))

	if err == nil {
		t.Errorf("Locate(%q, home %q) error = nil, want a refusal when the config home is the repo's .smith through a symlink", root, home)
	}
}

func TestLocateRefusesARepoWhoseSmithSymlinksToTheConfigHome(t *testing.T) {
	t.Parallel()
	for name, homeName := range map[string]string{
		"config home named .smith": ".smith",
		"custom config home path":  "smith-config",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			home := filepath.Join(t.TempDir(), homeName)
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(home, filepath.Join(root, ".smith")); err != nil {
				t.Fatal(err)
			}

			_, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home))

			if err == nil || !strings.Contains(err.Error(), home) {
				t.Errorf("Locate(%q, home %q) error = %v, want a refusal naming the config home", root, home, err)
			}
		})
	}
}

func TestLocateRefusesTheHomeDirectoryRepoBeforeTheConfigHomeExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, ".smith")

	_, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home))

	if err == nil || !strings.Contains(err.Error(), home) {
		t.Errorf("Locate(%q, home %q) error = %v, want a refusal naming the config home", root, home, err)
	}
}

func TestLocateAcceptsARepoWhoseSmithIsItsOwn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".smith"), 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), ".smith")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := repofile.Locate(t.Context(), fakeGit{root: root}, root, config.NewHome(home)); err != nil {
		t.Errorf("Locate(%q, home %q) error = %v, want a repo with its own .smith accepted", root, home, err)
	}
}
