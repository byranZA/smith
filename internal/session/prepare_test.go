package session_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// preparer stands in for worktree preparation. It records the worktree of
// each call along with how many tmux sessions had launched by then, writes say
// to the output it was handed, and answers with err.
type preparer struct {
	tmux     *tmuxServer
	dirs     []string
	launched []int
	say      string
	err      error
}

func (p *preparer) Prepare(_ context.Context, dir string, out io.Writer) error {
	p.dirs = append(p.dirs, dir)
	p.launched = append(p.launched, p.tmux.ran("new-session"))
	if _, err := io.WriteString(out, p.say); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return p.err
}

// preparedEnv is an Env over a workspace holding one bare repo "smith", with
// prep preparing its worktrees and progress receiving what Start prints. The
// workspace has its symlinks resolved, as git reports an existing worktree.
func preparedEnv(t *testing.T, prep *preparer, progress io.Writer) session.Env {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the workspace: %v", err)
	}
	writeBareRepo(t, workspace, "smith", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      prep.tmux,
		Preparer:  prep,
		Progress:  progress,
	}
}

func TestStartPreparesANewHumanSessionBeforeItLaunches(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}}
	env := preparedEnv(t, prep, io.Discard)

	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(env.Workspace, "smith", "worktrees", "spec-42")
	if want := []string{dir}; !reflect.DeepEqual(prep.dirs, want) {
		t.Errorf("Start() prepared %v, want %v", prep.dirs, want)
	}
	if want := []int{0}; !reflect.DeepEqual(prep.launched, want) {
		t.Errorf("Start() had launched %v tmux sessions at each preparation, want %v", prep.launched, want)
	}
}

func TestStartPreparesARestartedHumanSessionAgain(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}}
	env := preparedEnv(t, prep, io.Discard)
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(t.Context(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}
	prep.tmux.kill(session.TmuxSession("smith-spec-42"))

	if _, err := session.Start(t.Context(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(env.Workspace, "smith", "worktrees", "spec-42")
	if want := []string{dir, dir}; !reflect.DeepEqual(prep.dirs, want) {
		t.Errorf("Start() prepared %v, want %v", prep.dirs, want)
	}
	if want := []int{0, 1}; !reflect.DeepEqual(prep.launched, want) {
		t.Errorf("Start() had launched %v tmux sessions at each preparation, want %v", prep.launched, want)
	}
}

func TestStartDoesNotPrepareALiveSession(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}}
	env := preparedEnv(t, prep, io.Discard)
	req := session.StartRequest{Repo: "smith", Branch: "spec-42"}
	if _, err := session.Start(t.Context(), env, req); err != nil {
		t.Fatalf("first Start() err = %v", err)
	}

	if _, err := session.Start(t.Context(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if len(prep.dirs) != 1 {
		t.Errorf("Start() prepared %v, want only the first start to prepare", prep.dirs)
	}
}

func TestStartDoesNotPrepareALoopDrivenSession(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}}
	env := preparedEnv(t, prep, io.Discard)

	req := session.StartRequest{Repo: "smith", Branch: "spec-42", Command: []string{"smith", "loop", "run"}}
	if _, err := session.Start(t.Context(), env, req); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if len(prep.dirs) != 0 {
		t.Errorf("Start() prepared %v, want a loop-driven session left unprepared", prep.dirs)
	}
}

func TestStartStreamsPreparationOutputToItsProgress(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}, say: "mise node@22.1.0 ✓ installed\n"}
	var progress strings.Builder
	env := preparedEnv(t, prep, &progress)

	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	if got, want := progress.String(), "mise node@22.1.0 ✓ installed\n"; got != want {
		t.Errorf("Start() printed %q, want %q", got, want)
	}
}

func TestStartWarnsOfAFailedPreparation(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}, err: errors.New("the toolchain step exited with status 1: mise --cd dir install")}
	var progress strings.Builder
	env := preparedEnv(t, prep, &progress)

	if _, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(env.Workspace, "smith", "worktrees", "spec-42")
	want := "smith: warning: could not prepare the worktree at " + dir + ": the toolchain step exited with status 1: mise --cd dir install\n"
	if got := progress.String(); got != want {
		t.Errorf("Start() printed %q, want %q", got, want)
	}
}

func TestStartLaunchesASessionWhosePreparationFailed(t *testing.T) {
	t.Parallel()
	prep := &preparer{tmux: &tmuxServer{}, err: errors.New("the toolchain step exited with status 1: mise --cd dir install")}
	env := preparedEnv(t, prep, io.Discard)

	got, err := session.Start(t.Context(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	want := session.Session{Name: "smith-spec-42", Repo: "smith", Branch: "spec-42", Live: true}
	if got != want {
		t.Errorf("Start() = %+v, want %+v", got, want)
	}
	if n := prep.tmux.ran("new-session"); n != 1 {
		t.Errorf("tmux new-session ran %d times, want the session launched anyway", n)
	}
}
