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

// fakeGit reports the configured checkout root or bare-repo status.
type fakeGit struct {
	root string
	bare bool
}

func (f fakeGit) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name != "git" || len(args) != 4 || args[0] != "-C" || args[2] != "rev-parse" {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	switch {
	case args[3] == "--show-toplevel" && f.root != "":
		_, err := fmt.Fprintln(stdout, f.root)
		return err
	case args[3] == "--is-bare-repository" && f.root == "" && f.bare:
		_, err := fmt.Fprintln(stdout, "true")
		return err
	case args[3] == "--show-toplevel" || args[3] == "--is-bare-repository":
		return errors.New("exit status 128")
	}
	return fmt.Errorf("unexpected command %s %v", name, args)
}

// recordingGit is a fakeGit that records the rev-parse flag of each call it receives.
type recordingGit struct {
	fakeGit
	calls *[]string
}

func (r recordingGit) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	*r.calls = append(*r.calls, args[len(args)-1])
	return r.fakeGit.Run(ctx, name, args, stdin, stdout, stderr)
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

func TestLocateKeepsTheNotARepoMessageOutsideAGitRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	home := t.TempDir()

	_, err := repofile.Locate(t.Context(), fakeGit{}, dir, config.NewHome(home))

	if want := "locate the repo from " + dir + ": must be run inside a git repo: exit status 128"; err == nil || err.Error() != want {
		t.Errorf("Locate(%q, home %q) error = %v, want %q", dir, home, err, want)
	}
	if errors.Is(err, repofile.ErrBareRepo) {
		t.Errorf("Locate(%q, home %q) error = %v, want it not to match ErrBareRepo", dir, home, err)
	}
}

func TestLocateRefusesABareRepo(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "repo.git")
	home := t.TempDir()

	_, err := repofile.Locate(t.Context(), fakeGit{bare: true}, dir, config.NewHome(home))

	if !errors.Is(err, repofile.ErrBareRepo) || errors.Is(err, repofile.ErrNotARepo) {
		t.Fatalf("Locate(%q, home %q) error = %v, want ErrBareRepo and not ErrNotARepo", dir, home, err)
	}
	if want := dir + " is a bare repo: run from a checkout of it, or with --repo on a box"; !strings.Contains(err.Error(), want) {
		t.Errorf("Locate(%q, home %q) error = %q, want it to contain %q", dir, home, err, want)
	}
}

func TestLocateAsksGitOnlyForTheTopLevelInACheckout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := t.TempDir()
	var calls []string

	if _, err := repofile.Locate(t.Context(), recordingGit{fakeGit{root: root}, &calls}, root, config.NewHome(home)); err != nil {
		t.Fatalf("Locate(%q, home %q) error = %v", root, home, err)
	}

	if want := []string{"--show-toplevel"}; !slices.Equal(calls, want) {
		t.Errorf("Locate(%q, home %q) asked git %v, want %v", root, home, calls, want)
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
