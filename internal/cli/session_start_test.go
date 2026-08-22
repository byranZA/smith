package cli

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
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
