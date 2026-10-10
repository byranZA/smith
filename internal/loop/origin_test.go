package loop_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
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

func push(t *testing.T, dir string) (loop.Pushed, error) {
	t.Helper()
	return loop.Origin{Git: connection.System(), Dir: dir}.Push(context.Background())
}

func TestOriginPushSetsTheBranchUpstreamSoABareGitPushFollows(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
	git(t, clone, "commit", "--allow-empty", "-m", "task 43")

	pushed, err := push(t, clone)
	if err != nil || pushed != (loop.Pushed{Branch: "feat/42"}) {
		t.Fatalf("Push() = %+v, %v; want feat/42 pushed", pushed, err)
	}
	git(t, clone, "commit", "--allow-empty", "-m", "task 44")
	var stderr bytes.Buffer
	bare := exec.Command("git", "-C", clone, "push")
	bare.Stderr = &stderr

	if err := bare.Run(); err != nil || git(t, remote, "rev-parse", "feat/42") != git(t, clone, "rev-parse", "HEAD") {
		t.Errorf("bare git push after Push() = %v, %s; want it to push feat/42 to origin", err, stderr.String())
	}
}

func TestOriginSkipsOriginsDefaultBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		checkout func(t *testing.T, clone, remote string) string
	}{
		{"a clone on main", func(t *testing.T, clone, _ string) string { return clone }},
		{"a worktree of a bare clone, which sets no origin/HEAD", func(t *testing.T, _, remote string) string {
			root := t.TempDir()
			bare, tree := filepath.Join(root, "repo.git"), filepath.Join(root, "main")
			git(t, root, "clone", "--bare", "-c", "remote.origin.fetch=+refs/heads/*:refs/remotes/origin/*", remote, bare)
			git(t, bare, "worktree", "add", tree, "main")
			return tree
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clone, remote := cloned(t)
			dir := tt.checkout(t, clone, remote)
			before := git(t, remote, "rev-parse", "main")
			git(t, dir, "commit", "--allow-empty", "-m", "task 43")

			pushed, err := push(t, dir)

			if err != nil || pushed != (loop.Pushed{Branch: "main", Skipped: true}) || git(t, remote, "rev-parse", "main") != before {
				t.Errorf("Push() = %+v, %v; want main skipped and origin's main unchanged", pushed, err)
			}
		})
	}
}

func TestOriginPushesNothingWhenOriginNamesNoDefaultBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	remote, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	git(t, root, "init", "--bare", "-b", "main", remote)
	git(t, root, "clone", remote, clone)
	git(t, clone, "switch", "-c", "feat/42")
	git(t, clone, "commit", "--allow-empty", "-m", "task 43")

	_, err := push(t, clone)

	if err == nil || !strings.Contains(err.Error(), "origin names no default branch") || !strings.Contains(err.Error(), "git ls-remote --symref origin HEAD") || git(t, remote, "branch", "--list", "feat/42") != "" {
		t.Errorf("Push() error = %v; want it to name the missing default branch and its fix, with nothing pushed", err)
	}
}

// scriptedGit is a git that answers on branch feat/42 of a repo whose origin defaults to main, recording each push.
type scriptedGit struct {
	pushes [][]string
}

func (s *scriptedGit) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	switch args[2] {
	case "symbolic-ref":
		_, err := io.WriteString(stdout, "feat/42\n")
		return err
	case "ls-remote":
		_, err := io.WriteString(stdout, "ref: refs/heads/main\tHEAD\n0123abcd\tHEAD\n")
		return err
	case "push":
		s.pushes = append(s.pushes, args)
		return nil
	default:
		return fmt.Errorf("unscripted git %q", args)
	}
}

func TestOriginPushesWithoutForceToTheBranchOfTheSameName(t *testing.T) {
	t.Parallel()
	scripted := &scriptedGit{}

	if _, err := (loop.Origin{Git: scripted, Dir: "/work"}).Push(context.Background()); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	want := [][]string{{"-C", "/work", "push", "--set-upstream", "origin", "feat/42"}}
	if !reflect.DeepEqual(scripted.pushes, want) {
		t.Errorf("Push() ran git %q, want %q", scripted.pushes, want)
	}
}

func TestOriginPushFailsNamingWhatGitSaidWhenOriginRejectsIt(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	git(t, filepath.Dir(elsewhere), "clone", remote, elsewhere)
	git(t, elsewhere, "switch", "-c", "feat/42")
	git(t, elsewhere, "commit", "--allow-empty", "-m", "someone else's")
	git(t, elsewhere, "push", "origin", "feat/42")
	git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
	git(t, clone, "commit", "--allow-empty", "-m", "task 43")

	_, err := push(t, clone)

	if err == nil || !strings.Contains(err.Error(), "push feat/42 to origin") || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("Push() error = %v; want git's rejection of feat/42", err)
	}
}

func TestOriginPushWithNothingNewSucceeds(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
	if _, err := push(t, clone); err != nil {
		t.Fatalf("first Push() error = %v", err)
	}

	pushed, err := push(t, clone)

	if err != nil || pushed != (loop.Pushed{Branch: "feat/42"}) {
		t.Errorf("second Push() = %+v, %v; want feat/42 and no error", pushed, err)
	}
}
