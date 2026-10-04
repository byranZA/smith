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

// fakeGit answers `git -C <dir> rev-parse --show-toplevel` with root, or exits
// 128 the way git does outside a repo when root is empty.
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
	root := t.TempDir()
	sub := filepath.Join(root, "internal", "cli")

	repo, err := repofile.Locate(context.Background(), fakeGit{root: root}, sub, config.NewHome(t.TempDir()))
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}

	if want := filepath.Join(root, ".smith", "repo.yaml"); repo.Path() != want {
		t.Errorf("Path() = %q, want %q", repo.Path(), want)
	}
}

func TestLocateRefusesOutsideAGitRepo(t *testing.T) {
	_, err := repofile.Locate(context.Background(), fakeGit{}, t.TempDir(), config.NewHome(t.TempDir()))

	if !errors.Is(err, repofile.ErrNotARepo) {
		t.Errorf("Locate() error = %v, want ErrNotARepo", err)
	}
}

func TestLocateRefusesWhenTheReposSmithIsTheConfigHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".smith")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := repofile.Locate(context.Background(), fakeGit{root: root}, root, config.NewHome(home))

	if err == nil || !strings.Contains(err.Error(), home) {
		t.Errorf("Locate() error = %v, want a refusal naming the config home %s", err, home)
	}
}

func TestLocateRefusesTheConfigHomeReachedThroughASymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}

	_, err := repofile.Locate(context.Background(), fakeGit{root: root}, root, config.NewHome(filepath.Join(link, ".smith")))

	if err == nil {
		t.Error("Locate() error = nil, want a refusal when the config home is the repo's .smith through a symlink")
	}
}
