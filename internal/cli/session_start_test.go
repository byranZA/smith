package cli

import (
	"os"
	"path/filepath"
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
// terminal is handed to tmux writable.
func TestSessionStartConnectsWritableUnlessDetached(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	connect := &fakeExec{}

	_, stderr, code := runSessionOn(t,
		onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, connect),
		"start", "--repo", "smith", "--branch", "spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got, want := connect.line(t), "tmux attach-session -t smith/smith-spec-42"; got != want {
		t.Errorf("exec argv = %q, want %q", got, want)
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
// directory, both reach the stand-up. Which files a placement converges is
// internal/session's to prove.
func TestSessionStartCarriesTheStagedPlacementsIntoTheVerb(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	root := stageRepoPlacement(t, "smith", "config/.env", "TOKEN=staged\n")
	w := onBox(t, resolvedBoxPlacing(workspace, "smith", "config/.env"), root, connection.System(), &fakeTmux{}, &fakeExec{})

	_, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	placed := filepath.Join(workspace, "smith", "worktrees", "spec-42", "config", ".env")
	data, err := os.ReadFile(placed)
	if err != nil {
		t.Fatalf("read the placed file: %v", err)
	}
	if string(data) != "TOKEN=staged\n" {
		t.Errorf("placed file = %q, want the staged bytes", string(data))
	}
}
