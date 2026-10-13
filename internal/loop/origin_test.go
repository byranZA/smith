package loop_test

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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

func push(t *testing.T, dir, branch string) error {
	t.Helper()
	return loop.Origin{Git: connection.System(), Dir: dir}.Push(t.Context(), branch)
}

func TestOriginPushSetsTheBranchUpstreamSoABareGitPushFollows(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
	git(t, clone, "commit", "--allow-empty", "-m", "task 43")

	if err := push(t, clone, "feat/42"); err != nil {
		t.Fatalf("Push(feat/42) error = %v", err)
	}
	git(t, clone, "commit", "--allow-empty", "-m", "task 44")
	var stderr bytes.Buffer
	bare := exec.Command("git", "-C", clone, "push")
	bare.Stderr = &stderr

	if err := bare.Run(); err != nil || git(t, remote, "rev-parse", "feat/42") != git(t, clone, "rev-parse", "HEAD") {
		t.Errorf("bare git push after Push() = %v, %s; want it to push feat/42 to origin", err, stderr.String())
	}
}

func TestOriginPushesTheBranchToItsOwnNameOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, clone string)
	}{
		{"a plain feature branch", func(*testing.T, string) {}},
		{"a feature branch with a tag also named feat/42", func(t *testing.T, clone string) {
			git(t, clone, "tag", "feat/42")
		}},
		{"push config mapping the branch onto main", func(t *testing.T, clone string) {
			git(t, clone, "config", "remote.origin.push", "refs/heads/feat/42:refs/heads/main")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clone, remote := cloned(t)
			main := git(t, remote, "rev-parse", "refs/heads/main")
			git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
			git(t, clone, "commit", "--allow-empty", "-m", "task 43")
			tt.setup(t, clone)

			err := push(t, clone, "feat/42")

			got := []string{
				git(t, remote, "rev-parse", "refs/heads/main"),
				git(t, remote, "for-each-ref", "--format=%(objectname)", "refs/heads/feat/42"),
				git(t, clone, "for-each-ref", "--format=%(upstream)", "refs/heads/feat/42"),
			}
			want := []string{main, git(t, clone, "rev-parse", "refs/heads/feat/42"), "refs/remotes/origin/feat/42"}
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("Push(feat/42) error = %v; origin's main, origin's feat/42 and the upstream = %q, want %q", err, got, want)
			}
		})
	}
}

// scriptedGit is a git that records each command it is asked to run.
type scriptedGit struct {
	ran [][]string
}

func (s *scriptedGit) Run(_ context.Context, _ string, args []string, _ io.Reader, _, _ io.Writer) error {
	s.ran = append(s.ran, args)
	return nil
}

func TestOriginPushesTheNamedBranchWhileAnotherIsCheckedOut(t *testing.T) {
	t.Parallel()
	clone, remote := cloned(t)
	work := git(t, clone, "commit-tree", "-p", "HEAD", "-m", "task 43", "HEAD^{tree}")
	git(t, clone, "branch", "smith/42-spec", work)

	err := push(t, clone, "smith/42-spec")

	if got := git(t, remote, "for-each-ref", "--format=%(objectname)", "refs/heads/smith/42-spec"); err != nil || got != work {
		t.Errorf("Push(smith/42-spec) error = %v, origin's smith/42-spec = %q; want it pushed at %s", err, got, work)
	}
}

func TestOriginPushesWithoutForceToTheBranchOfTheSameName(t *testing.T) {
	t.Parallel()
	scripted := &scriptedGit{}

	if err := (loop.Origin{Git: scripted, Dir: "/work"}).Push(t.Context(), "feat/42"); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	want := [][]string{{"-C", "/work", "push", "--set-upstream", "origin", "refs/heads/feat/42:refs/heads/feat/42"}}
	if !reflect.DeepEqual(scripted.ran, want) {
		t.Errorf("Push(feat/42) ran git %q, want %q", scripted.ran, want)
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

	err := push(t, clone, "feat/42")

	if err == nil || !strings.Contains(err.Error(), "push feat/42 to origin") || !strings.Contains(err.Error(), "rejected") {
		t.Errorf("Push() error = %v; want git's rejection of feat/42", err)
	}
}

func TestOriginPushWithNothingNewSucceeds(t *testing.T) {
	t.Parallel()
	clone, _ := cloned(t)
	git(t, clone, "switch", "--no-track", "-c", "feat/42", "origin/main")
	if err := push(t, clone, "feat/42"); err != nil {
		t.Fatalf("first Push(feat/42) error = %v", err)
	}

	if err := push(t, clone, "feat/42"); err != nil {
		t.Errorf("second Push(feat/42) error = %v, want none", err)
	}
}
