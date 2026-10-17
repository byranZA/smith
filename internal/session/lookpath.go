package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LookPath finds the named executable where the root process of a session on
// branch of repo would: on the PATH mise exec gives a process in that
// session's worktree, or in the nearest directory above it while the worktree
// does not exist yet, so mise reads the config every worktree of the repo
// inherits. It creates nothing, and runner is what runs mise.
func LookPath(ctx context.Context, env Env, runner Runner, repo, branch, name string) (string, error) {
	r, err := env.repo(repo)
	if err != nil {
		return "", err
	}
	dir := nearest(filepath.Join(env.Workspace, r.Name, worktreesDir, worktreeDir(branch)))
	path, err := run(ctx, runner, cmp.Or(env.Mise, miseFile), "-C", dir, "exec", "--", "sh", "-c", `command -v "$1"`, "sh", name)
	if err != nil {
		return "", fmt.Errorf("find %s on the PATH mise gives a session in %s: %w", name, dir, err)
	}
	return path, nil
}

// nearest is dir, or else its closest ancestor that exists.
func nearest(dir string) string {
	for {
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
