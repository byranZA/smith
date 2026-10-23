package session_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// fakeMise stands in for mise, recording the argv it was handed and answering
// the lookup with path, or failing as command -v does when path is empty.
type fakeMise struct {
	path  string
	calls [][]string
}

func (f *fakeMise) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.path == "" {
		return errors.New("exit status 1")
	}
	_, err := io.WriteString(stdout, f.path+"\n")
	return err
}

// lookPathEnv is a box whose workspace holds the bare clone of "smith", with mise at its home install.
func lookPathEnv(t *testing.T) session.Env {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &fakeTmux{},
		Mise:      "/home/smith/.local/bin/mise",
	}
}

func TestLookPathAnswersWithWhereMiseFindsTheExecutable(t *testing.T) {
	t.Parallel()
	mise := &fakeMise{path: "/home/smith/.local/share/mise/installs/claude/1.0/bin/claude"}

	got, err := session.LookPath(t.Context(), lookPathEnv(t), mise, "smith", "spec-42", "claude")

	if want := "/home/smith/.local/share/mise/installs/claude/1.0/bin/claude"; err != nil || got != want {
		t.Errorf("LookPath() = %q, %v; want %q", got, err, want)
	}
}

func TestLookPathBeforeTheWorktreeExistsRunsMiseInTheRepoDirectory(t *testing.T) {
	t.Parallel()
	env := lookPathEnv(t)
	mise := &fakeMise{path: "/usr/bin/claude"}

	if _, err := session.LookPath(t.Context(), env, mise, "smith", "spec-42", "claude"); err != nil {
		t.Fatalf("LookPath() err = %v", err)
	}

	want := [][]string{{"/home/smith/.local/bin/mise", "-C", filepath.Join(env.Workspace, "smith"), "exec", "--", "sh", "-c", `command -v "$1"`, "sh", "claude"}}
	if !reflect.DeepEqual(mise.calls, want) {
		t.Errorf("LookPath() ran %q, want %q", mise.calls, want)
	}
}

func TestLookPathInAnExistingWorktreeRunsMiseThere(t *testing.T) {
	t.Parallel()
	env := lookPathEnv(t)
	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "feat/x"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	mise := &fakeMise{path: "/usr/bin/claude"}

	if _, err := session.LookPath(t.Context(), env, mise, "smith", "feat/x", "claude"); err != nil {
		t.Fatalf("LookPath() err = %v", err)
	}

	want := [][]string{{"/home/smith/.local/bin/mise", "-C", filepath.Join(env.Workspace, "smith", "worktrees", "feat-x"), "exec", "--", "sh", "-c", `command -v "$1"`, "sh", "claude"}}
	if !reflect.DeepEqual(mise.calls, want) {
		t.Errorf("LookPath() ran %q, want %q", mise.calls, want)
	}
}

func TestLookPathOfAMissingExecutableFails(t *testing.T) {
	t.Parallel()

	got, err := session.LookPath(t.Context(), lookPathEnv(t), &fakeMise{}, "smith", "spec-42", "claude")

	if err == nil {
		t.Errorf("LookPath() = %q, nil; want an error", got)
	}
}

func TestLookPathRefusesAnUndeclaredRepo(t *testing.T) {
	t.Parallel()
	mise := &fakeMise{path: "/usr/bin/claude"}

	_, err := session.LookPath(t.Context(), lookPathEnv(t), mise, "nope", "spec-42", "claude")

	if err == nil || len(mise.calls) != 0 {
		t.Errorf("LookPath() err = %v after mise %q; want a refusal and no mise run", err, mise.calls)
	}
}

func TestUnderMiseRunsEachCommandUnderMiseExec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mise string
		want [][]string
	}{
		{"mise at its home install", "/home/smith/.local/bin/mise", [][]string{{"/home/smith/.local/bin/mise", "exec", "--", "gh", "issue", "view", "42"}}},
		{"mise by name on PATH", "", [][]string{{"mise", "exec", "--", "gh", "issue", "view", "42"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mise := &fakeMise{path: "/usr/bin/gh"}

			if err := session.UnderMise(session.Env{Mise: tt.mise}, mise).Run(t.Context(), "gh", []string{"issue", "view", "42"}, nil, io.Discard, io.Discard); err != nil {
				t.Fatalf("UnderMise().Run() err = %v", err)
			}

			if !reflect.DeepEqual(mise.calls, tt.want) {
				t.Errorf("UnderMise(%q).Run(gh issue view 42) ran %q, want %q", tt.mise, mise.calls, tt.want)
			}
		})
	}
}

func TestUnderMiseStreamsWhatTheCommandWrites(t *testing.T) {
	t.Parallel()
	mise := &fakeMise{path: "/usr/bin/gh"}
	var stdout strings.Builder

	if err := session.UnderMise(session.Env{}, mise).Run(t.Context(), "gh", []string{"issue", "view", "42"}, nil, &stdout, io.Discard); err != nil {
		t.Fatalf("UnderMise().Run() err = %v", err)
	}

	if got, want := stdout.String(), "/usr/bin/gh\n"; got != want {
		t.Errorf("UnderMise().Run() wrote %q, want %q", got, want)
	}
}
