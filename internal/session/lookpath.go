package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
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
	argv := env.mise(r.Name, "-C", dir, "exec", "--", "sh", "-c", `command -v "$1"`, "sh", name)
	path, err := run(ctx, runner, argv[0], argv[1:]...)
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

// MiseExec is a Runner that launches every command under the mise exec a
// session's process in a repo runs under, with mise reading its config from the
// directory it was given, or from the one its runner launches from.
type MiseExec struct {
	env    Env
	repo   string
	dir    string
	runner Runner
}

// UnderMise is runner with every command it launches put under the mise exec
// a session's process in repo on env's box runs under, so it finds the tools
// and environment the blueprint declares where a relayed smith's own PATH would
// not. mise reads its config from dir, or, given an empty dir, from wherever
// runner launches it.
func UnderMise(env Env, runner Runner, repo, dir string) MiseExec {
	return MiseExec{env: env, repo: repo, dir: dir, runner: runner}
}

// Run launches name with args under mise exec, wiring the given
// stdin/stdout/stderr, and returns the process error.
func (m MiseExec) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	exec := append([]string{"exec", "--", name}, args...)
	if m.dir != "" {
		exec = append([]string{"-C", m.dir}, exec...)
	}
	argv := m.env.mise(m.repo, exec...)
	if err := m.runner.Run(ctx, argv[0], argv[1:], stdin, stdout, stderr); err != nil {
		return fmt.Errorf("run %s under %s exec: %w", name, cmp.Or(m.env.Mise, miseFile), err)
	}
	return nil
}
