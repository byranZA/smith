package session_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// registryEnv declares two repos and clones both, so a test can pin that every
// verb reads the same registry rather than only the repo it was handed.
func registryEnv(t *testing.T) session.Env {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	writeBareRepo(t, workspace, "web", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}, {Name: "web"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
		Exec:      &fakeExec{},
	}
}

// TestEveryVerbAddressesTheNameListReports locks in that the five verbs share
// one registry: the name list prints for a session in the second declared repo
// is the name attach, stop and rm resolve, and the branch each of them reads
// back is the one git holds rather than the one the name spells.
func TestEveryVerbAddressesTheNameListReports(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	started, err := session.Start(ctx, env, session.StartRequest{Repo: "web", Branch: "web/spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	listed, err := session.List(ctx, env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(listed) != 1 || listed[0].Name != started.Name || listed[0].Branch != "web/spec-42" {
		t.Fatalf("List() = %+v, want one %q on branch %q", listed, started.Name, "web/spec-42")
	}
	name := listed[0].Name

	if err := session.Attach(ctx, env, name, session.Interact, io.Discard); err != nil {
		t.Fatalf("Attach(%q) err = %v", name, err)
	}
	if err := session.Stop(ctx, env, name); err != nil {
		t.Fatalf("Stop(%q) err = %v", name, err)
	}
	removed, err := session.Remove(ctx, env, []string{name}, false)
	if err != nil {
		t.Fatalf("Remove(%q) err = %v", name, err)
	}
	if len(removed) != 1 || removed[0].Repo != "web" || removed[0].Branch != "web/spec-42" {
		t.Fatalf("Remove() = %+v, want the web repo's branch %q", removed, "web/spec-42")
	}
}

// TestEveryVerbRefusesAnUnknownNameAlike locks in that name resolution has one
// home: a name no session holds reads the same at every verb that takes one,
// so a typo cannot mean three different things.
func TestEveryVerbRefusesAnUnknownNameAlike(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	if _, err := session.Start(ctx, env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	refusals := map[string]error{
		"attach": session.Attach(ctx, env, "smith-ghost", session.ByDriver, io.Discard),
		"stop":   session.Stop(ctx, env, "smith-ghost"),
	}
	_, refusals["rm"] = session.Remove(ctx, env, []string{"smith-ghost"}, false)

	for verb, err := range refusals {
		if err == nil {
			t.Fatalf("%s of an unknown name err = nil, want a refusal", verb)
		}
		for _, want := range []string{"smith-ghost", "session list"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s of an unknown name err = %q, want it to contain %q", verb, err, want)
			}
		}
	}
}

// taskWorktree stands up a worktree where the loop puts its task worktrees,
// inside the bare clone rather than below the repo's worktrees directory, and
// returns its path.
func taskWorktree(t *testing.T, env session.Env) string {
	t.Helper()
	bare := filepath.Join(env.Workspace, "smith", "repo.git")
	dir := filepath.Join(bare, "smith", "worktrees", "smith-work", "7")
	git(t, bare, "worktree", "add", "-b", "smith/tasks/7", dir, "main")
	return dir
}

func TestListWithholdsAWorktreeOutsideTheWorktreesDir(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	started, err := session.Start(ctx, env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	taskWorktree(t, env)

	listed, err := session.List(ctx, env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	var names []string
	for _, s := range listed {
		names = append(names, s.Name)
	}
	if want := []string{started.Name}; !equal(names, want) {
		t.Errorf("List() names = %q, want %q", names, want)
	}
}

func TestEveryVerbRefusesAWorktreeOutsideTheWorktreesDirAsUnknown(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	dir := taskWorktree(t, env)
	const name = "smith-smith-tasks-7"

	refusals := map[string]error{
		"attach": session.Attach(ctx, env, name, session.Interact, io.Discard),
		"stop":   session.Stop(ctx, env, name),
	}
	_, refusals["rm"] = session.Remove(ctx, env, []string{name}, false)
	_, refusals["rm --force"] = session.Remove(ctx, env, []string{name}, true)

	want := "no session named \"smith-smith-tasks-7\" exists on this box: `smith session list` reports the sessions there are"
	for verb, err := range refusals {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s(%q) err = %v, want the unknown-session refusal %q", verb, name, err, want)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the task worktree at %s is gone after the refusals: %v", dir, err)
	}
}

func TestRemoveRefusesABatchNamingAWorktreeOutsideTheWorktreesDirWhole(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	started, err := session.Start(ctx, env, session.StartRequest{Repo: "smith", Branch: "spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	if err := session.Stop(ctx, env, started.Name); err != nil {
		t.Fatalf("Stop(%q) err = %v", started.Name, err)
	}
	task := taskWorktree(t, env)

	_, err = session.Remove(ctx, env, []string{started.Name, "smith-smith-tasks-7"}, true)

	if err == nil {
		t.Fatal("Remove() err = nil, want the batch refused for its unknown name")
	}
	for _, dir := range []string{filepath.Join(env.Workspace, "smith", "worktrees", "spec-42"), task} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("worktree at %s is gone after a refused batch: %v", dir, err)
		}
	}
}
