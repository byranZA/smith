package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/misetrust"
)

// TestSessionStartDetachedReportsTheSessionAndConnectsToNothing is the smoke
// test for the verb: the flags an operator types reach a stand-up that
// succeeds, the name the other verbs take is on stdout, and --detach is
// honoured by connecting to nothing. What the stand-up did to the box is
// internal/session's to prove.
func TestSessionStartDetachedReportsTheSessionAndConnectsToNothing(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	connect := &fakeExec{}

	stdout, stderr, code := runSessionOn(t,
		onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, connect),
		"start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want it to name the session", stdout)
	}
	if len(connect.calls) != 0 {
		t.Errorf("exec argv = %v, want --detach to connect to nothing", connect.calls)
	}
}

// TestSessionStartConnectsWritableUnlessDetached locks in the connect half of
// start: standing a session up and being put in it is one command, and the
// terminal is handed over the way `attach --interact` hands it over rather
// than at the read-only access level. What that connection looks like is
// internal/session's to prove, so the attach verb is read as the answer.
func TestSessionStartConnectsWritableUnlessDetached(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	started, writable := &fakeExec{}, &fakeExec{}

	_, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &fakeTmux{}, started),
		"start", "--repo", "smith", "--branch", "spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	mustAttach(t, resolve, writable, "smith-spec-42", "--interact")
	if got, want := started.line(t), writable.line(t); got != want {
		t.Errorf("start connected as %q, want the writable connection %q", got, want)
	}
}

// TestSessionStartIsUnderTheRootCommand locks in the surface an operator on
// the box types.
func TestSessionStartIsUnderTheRootCommand(t *testing.T) {
	underRoot(t, "start")
}

// TestSessionStartCarriesTheStagedPlacementsIntoTheVerb locks in the wiring
// only the command assembles: the placements declared in the blueprint this
// box was built from, and the bytes `machine setup` staged under its state
// directory, both reach the stand-up and land in the workspace. Where in the
// workspace a placement lands is internal/session's to prove.
func TestSessionStartCarriesTheStagedPlacementsIntoTheVerb(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	root := stageRepoPlacement(t, "smith", "config/.env", "TOKEN=staged\n")
	w := onBox(t, resolvedBoxPlacing(workspace, "smith", "config/.env"), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if placed := findInWorkspace(t, workspace, "TOKEN=staged\n"); placed == "" {
		t.Error("no file in the workspace holds the staged bytes, want the placement carried into the stand-up")
	}
}

// fakeMise stands in for the mise binary preparation installs a toolchain
// with: it records the argv it was handed, writes say to stdout, and answers
// with err.
type fakeMise struct {
	calls [][]string
	say   string
	err   error
}

func (f *fakeMise) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if _, err := io.WriteString(stdout, f.say); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return f.err
}

func TestSessionStartStreamsTheToolchainInstallToStderr(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{})
	mise := &fakeMise{say: "mise node@22.1.0 ✓ installed\n"}
	w.mise = mise

	_, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "mise node@22.1.0 ✓ installed\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if len(mise.calls) != 1 || !strings.HasSuffix(strings.Join(mise.calls[0], " "), "spec-42 install") {
		t.Errorf("mise ran %v, want one install pointed at the spec-42 worktree", mise.calls)
	}
}

func TestSessionStartPreparesTrustingTheReposDirectory(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{})
	mise := &fakeMise{}
	w.mise = mise

	_, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := []string{"sh", "-c", misetrust.Script, filepath.Join(workspace, "smith")}
	if len(mise.calls) != 1 || !slices.Equal(mise.calls[0][:len(want)], want) {
		t.Errorf("mise ran %q, want the install beginning %q", mise.calls, want)
	}
}

func TestSessionStartWarnsOfAFailedToolchainInstallAndStillStarts(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{})
	w.mise = &fakeMise{err: exitStatus(1)}

	stdout, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "the toolchain step exited with status 1"; !strings.HasPrefix(stderr, "smith: warning: ") || !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want a smith warning containing %q", stderr, want)
	}
	if !strings.Contains(stdout, "smith-spec-42") {
		t.Errorf("stdout = %q, want the session started anyway", stdout)
	}
}

// exitStatus is a process that ran and exited with that status.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e exitStatus) ExitCode() int { return int(e) }

func TestSessionStartRunsThePrepareCommandTheBranchDeclares(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepoWithRepoFiles(t, workspace, "smith", "prepare: pip install -r requirements.txt\n", "spec-42", "prepare: uv sync\n")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{})
	mise := &fakeMise{}
	w.mise = mise

	_, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(mise.calls) != 2 || !strings.HasSuffix(strings.Join(mise.calls[1], " "), "spec-42 exec -- sh -c uv sync") {
		t.Errorf("mise ran %v, want the install then the branch's own prepare command in the spec-42 worktree", mise.calls)
	}
}

// writeBareRepoWithRepoFiles is writeBareRepo whose main commits base as its
// repo file and whose branch commits its own repo file over it.
func writeBareRepoWithRepoFiles(t *testing.T, workspace, repo, base, branch, branchFile string) {
	t.Helper()
	src := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "smith@example.com"},
		{"config", "user.name", "smith"},
	} {
		runGit(t, src, args...)
	}
	commitRepoFile := func(content, message string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(src, ".smith"), 0o755); err != nil {
			t.Fatalf("make .smith: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, ".smith", "repo.yaml"), []byte(content), 0o600); err != nil {
			t.Fatalf("write repo file: %v", err)
		}
		runGit(t, src, "add", ".smith/repo.yaml")
		runGit(t, src, "commit", "-m", message)
	}
	commitRepoFile(base, "base")
	runGit(t, src, "checkout", "-b", branch)
	commitRepoFile(branchFile, "branch")
	runGit(t, src, "checkout", "main")
	if err := os.MkdirAll(filepath.Join(workspace, repo), 0o755); err != nil {
		t.Fatalf("make repo directory: %v", err)
	}
	runGit(t, src, "clone", "--bare", "-c", "remote.origin.fetch=+refs/heads/*:refs/remotes/origin/*", src, filepath.Join(workspace, repo, "repo.git"))
}
