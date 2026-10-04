package repofile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/config"
)

// dirName is the directory at a repo's root the repo file lives in, named to
// match the config home.
const dirName = ".smith"

// fileName is the repo file's name inside dirName.
const fileName = "repo.yaml"

// ErrNotARepo is returned by Locate when the directory is not inside a git repo.
var ErrNotARepo = errors.New("must be run inside a git repo")

// Runner launches a command on the operator's machine. It is the system
// boundary git is reached through, injected so a test can stand in for git.
type Runner interface {
	// Run launches name with args, wiring the given stdin/stdout/stderr, and
	// returns the process error.
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Repo is a located git repo, the one its repo file belongs to.
type Repo struct {
	root string
}

// Path is where the repo's repo file is, whether or not it exists yet.
func (r Repo) Path() string { return filepath.Join(r.root, dirName, fileName) }

// Locate finds the root of the git repo dir is inside, asking git. It returns
// ErrNotARepo when git finds none, and refuses a repo whose .smith directory
// is the config home, so operator config and repo config never share one.
func Locate(ctx context.Context, git Runner, dir string, home config.Home) (Repo, error) {
	var stdout bytes.Buffer
	if err := git.Run(ctx, "git", []string{"-C", dir, "rev-parse", "--show-toplevel"}, nil, &stdout, io.Discard); err != nil {
		return Repo{}, fmt.Errorf("locate the repo from %s: %w: %w", dir, ErrNotARepo, err)
	}
	root := strings.TrimSpace(stdout.String())
	if isConfigHome(root, home) {
		return Repo{}, fmt.Errorf("the repo's %s is the config home %s: keep repo config out of it", dirName, home.Path())
	}
	return Repo{root: root}, nil
}

// isConfigHome reports whether root's .smith directory is the config home,
// comparing the directories themselves so a symlinked path is still caught.
func isConfigHome(root string, home config.Home) bool {
	if filepath.Base(home.Path()) != dirName {
		return false
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	parentInfo, err := os.Stat(filepath.Dir(home.Path()))
	if err != nil {
		return false
	}
	return os.SameFile(rootInfo, parentInfo)
}
