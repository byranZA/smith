package workspace_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/workspace"
)

// trackingRefspec is the fetch refspec that keeps a bare clone's
// refs/remotes/origin/* current.
const trackingRefspec = "+refs/heads/*:refs/remotes/origin/*"

// gitBox is a box whose mise is present and runs git for real, so a test sees
// what converge leaves in an actual clone. Every other mise command succeeds
// without doing anything.
type gitBox struct{}

func (gitBox) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if name == "mise" && len(args) > 3 && slices.Equal(args[:3], []string{"exec", "--", "git"}) {
		return connection.System().Run(ctx, "git", args[3:], stdin, stdout, stderr)
	}
	return nil
}

// tmux stands in for the tmux binary a session is launched under.
type tmux struct{}

func (tmux) Run(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error {
	return nil
}

// remote makes a repo with one commit on main for a clone to be made from.
func remote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	commitIn(t, dir, "base")
	return dir
}

// commitIn puts a commit on whatever branch dir has checked out.
func commitIn(t *testing.T, dir, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, message+".txt"), []byte(message+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", message, err)
	}
	gitIn(t, dir, "add", message+".txt")
	gitIn(t, dir, "commit", "-m", message)
}

// gitIn runs a real git command in dir and returns its trimmed stdout.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var out, errOut strings.Builder
	full := append([]string{"-C", dir, "-c", "user.email=smith@example.com", "-c", "user.name=smith"}, args...)
	if err := connection.System().Run(context.Background(), "git", full, nil, &out, &errOut); err != nil {
		t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

// convergeRepo runs the stage for one repo cloned from url into home's
// workspace, and answers with what the repos step said it did.
func convergeRepo(t *testing.T, home, url string) string {
	t.Helper()
	b := blueprint.Blueprint{Repos: []blueprint.Repo{{Name: "acme", URL: url}}}
	r := staging.Resolution{Access: "public", Terminal: "tmux", Workspace: "~/workspace"}
	env := workspace.Env{Command: gitBox{}, StateRoot: t.TempDir()}
	result, err := workspace.Converge(context.Background(), env, workspace.Plan(b, r, home), io.Discard)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	for _, o := range result.Outcomes {
		if o.Step != workspace.Repos {
			continue
		}
		if o.Err != nil {
			t.Fatalf("Converge() repos step failed: %v", o.Err)
		}
		return o.Summary
	}
	t.Fatalf("Converge() = %+v, want a repos outcome", result)
	return ""
}

// bareAt is where the acme clone lands under home.
func bareAt(home string) string {
	return filepath.Join(home, "workspace", "acme", "repo.git")
}

// cloneWithoutRefspec bare-clones url into home's workspace the way converge
// did before it set a refspec, with no remote-tracking refs at all.
func cloneWithoutRefspec(t *testing.T, home, url string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(bareAt(home)), 0o700); err != nil {
		t.Fatalf("make the repo directory: %v", err)
	}
	gitIn(t, home, "clone", "--bare", url, bareAt(home))
}

func TestConvergeClonesWithARefspecThatTracksTheRemote(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	convergeRepo(t, home, remote(t))

	if got := gitIn(t, bareAt(home), "config", "--get-all", "remote.origin.fetch"); got != trackingRefspec {
		t.Errorf("remote.origin.fetch of a new clone = %q, want %q", got, trackingRefspec)
	}
}

func TestConvergeAddsTheMissingRefspecToAnExistingClone(t *testing.T) {
	t.Parallel()
	home, url := t.TempDir(), remote(t)
	cloneWithoutRefspec(t, home, url)

	convergeRepo(t, home, url)

	if got := gitIn(t, bareAt(home), "config", "--get-all", "remote.origin.fetch"); got != trackingRefspec {
		t.Errorf("remote.origin.fetch of a repaired clone = %q, want %q", got, trackingRefspec)
	}
}

func TestConvergeReportsTheRefspecItAdded(t *testing.T) {
	t.Parallel()
	home, url := t.TempDir(), remote(t)
	cloneWithoutRefspec(t, home, url)

	got := convergeRepo(t, home, url)

	want := "added the remote-tracking refspec to " + bareAt(home) + "; fetched " + bareAt(home)
	if got != want {
		t.Errorf("repos summary = %q, want %q", got, want)
	}
}

func TestConvergeReportsNoRepairForAClonePastTheRefspec(t *testing.T) {
	t.Parallel()
	home, url := t.TempDir(), remote(t)
	convergeRepo(t, home, url)

	got := convergeRepo(t, home, url)

	if want := "fetched " + bareAt(home); got != want {
		t.Errorf("repos summary on a second run = %q, want %q", got, want)
	}
}

func TestConvergeLeavesTheClonesLocalBranchesWhereTheyWere(t *testing.T) {
	t.Parallel()
	home, url := t.TempDir(), remote(t)
	cloneWithoutRefspec(t, home, url)
	before := gitIn(t, bareAt(home), "for-each-ref", "refs/heads")
	commitIn(t, url, "moved")

	convergeRepo(t, home, url)

	if got := gitIn(t, bareAt(home), "for-each-ref", "refs/heads"); got != before {
		t.Errorf("local branches after converge = %q, want them left at %q", got, before)
	}
}

func TestStartCutsFromTheRemoteTipConvergeFetched(t *testing.T) {
	t.Parallel()
	home, url := t.TempDir(), remote(t)
	convergeRepo(t, home, url)
	commitIn(t, url, "moved")
	convergeRepo(t, home, url)
	env := session.Env{
		Workspace: filepath.Join(home, "workspace"),
		Repos:     []session.Repo{{Name: "acme"}},
		Git:       connection.System(),
		Tmux:      tmux{},
	}

	if _, err := session.Start(context.Background(), env, session.StartRequest{Repo: "acme", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	dir := filepath.Join(home, "workspace", "acme", "worktrees", "spec-42")
	if got, want := gitIn(t, dir, "rev-parse", "HEAD"), gitIn(t, url, "rev-parse", "main"); got != want {
		t.Errorf("new branch is at %s, want the remote's new tip %s", got, want)
	}
}
