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

// tmuxServer stands in for a tmux server, which no test may depend on: it
// remembers the sessions new-session created and answers has-session from
// that, so a test can drive the live/stopped probe the way the box does.
type tmuxServer struct {
	live map[string]bool
}

func (s *tmuxServer) Run(_ context.Context, _ string, args []string, _ io.Reader, _, _ io.Writer) error {
	if s.live == nil {
		s.live = map[string]bool{}
	}
	switch {
	case len(args) > 0 && args[0] == "new-session":
		s.live[target(args)] = true
		return nil
	case len(args) > 0 && args[0] == "has-session":
		if s.live[target(args)] {
			return nil
		}
		return errNoSession
	default:
		return nil
	}
}

// kill forgets a tmux session the way a tmux server dying outside smith does.
func (s *tmuxServer) kill(name string) {
	delete(s.live, name)
}

// target returns the -t/-s value in a tmux argv.
func target(args []string) string {
	for i, a := range args {
		if (a == "-t" || a == "-s") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// errNoSession is what tmux exiting non-zero on has-session looks like here.
var errNoSession = errNoSessionType("no server running")

type errNoSessionType string

func (e errNoSessionType) Error() string { return string(e) }

// TestListReportsAStartedSessionWithItsTrueBranch locks in that the registry
// is the bare repo: the name is the sanitized one the other verbs take, and
// the branch comes back as git holds it rather than as the name spells it.
func TestListReportsAStartedSessionWithItsTrueBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "smith/spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	want := []session.Session{{Name: "smith-smith-spec-42", Repo: "smith", Branch: "smith/spec-42", Live: true}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}

// TestListProbesTmuxForEveryCall locks in that running state is probed rather
// than remembered: a tmux session that dies outside smith lists as stopped.
func TestListProbesTmuxForEveryCall(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	tmux := &tmuxServer{}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      tmux,
	}
	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}
	tmux.kill(session.TmuxSession("smith-spec-42"))

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("List() = %+v, want one session", got)
	}
	if got[0].Live {
		t.Errorf("List() reported %q live, want stopped once its tmux session is gone", got[0].Name)
	}
}

// TestListIgnoresADirectoryGitDoesNotKnowAbout locks in that debris under the
// worktrees directory is not a session: inventing a row for it would make list
// disagree with rm.
func TestListIgnoresADirectoryGitDoesNotKnowAbout(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	if err := os.MkdirAll(filepath.Join(workspace, "smith", "worktrees", "debris"), 0o755); err != nil {
		t.Fatalf("make debris directory: %v", err)
	}
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("List() = %+v, want no session for a directory git does not know about", got)
	}
}

// TestListSortsByRepoThenBranch locks in the one order the rows are ever in,
// whatever order the blueprint declared the repos in.
func TestListSortsByRepoThenBranch(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "web", "main")
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "web"}, {Name: "smith"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}
	for _, req := range []session.StartRequest{
		{Repo: "web", Branch: "hotfix"},
		{Repo: "smith", Branch: "spec-9"},
		{Repo: "smith", Branch: "spec-42"},
	} {
		if _, err := session.Start(context.Background(), env, req); err != nil {
			t.Fatalf("Start(%+v) err = %v", req, err)
		}
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"smith-spec-42", "smith-spec-9", "web-hotfix"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("List() names = %v, want %v", names, want)
	}
}

// TestListSkipsARepoThatIsNotClonedYet locks in that list exits zero whatever
// it finds: a declared repo the workspace stage has not cloned yet has no
// sessions, which is not a failure.
func TestListSkipsARepoThatIsNotClonedYet(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	env := session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}, {Name: "ghost"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
	}

	got, err := session.List(context.Background(), env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}

	if len(got) != 0 {
		t.Errorf("List() = %+v, want nothing listed for a workspace with no worktrees", got)
	}
}

var _ session.Runner = (*tmuxServer)(nil)
