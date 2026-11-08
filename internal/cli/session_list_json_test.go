package cli

import (
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
	"path/filepath"
	"strings"
	"testing"
)

func evalWorkspace(t *testing.T, workspace string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	return real
}

func TestSessionListJSONPrintsALiveSessionWithItsState(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	resolve := resolvedBox(workspace, "smith")
	tmux := &tmuxBox{}
	standUp(t, resolve, tmux, "smith", "spec-42")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, tmux, &fakeExec{}), "list", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"sessions":[{"name":"smith-spec-42","repo":"smith","branch":"spec-42","worktree":"` +
		evalWorkspace(t, workspace) + `/smith/worktrees/spec-42","live":true,"dirty":false,"unpushed":0,"driver":"human"}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestSessionListJSONOfAStoppedLoopSessionNamesTheLoopAsDriver(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	standUpLoop(t, workspace, "smith", "spec-42")
	resolve := resolvedBox(workspace, "smith")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolve, &tmuxBox{}, &fakeExec{}), "list", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"sessions":[{"name":"smith-spec-42","repo":"smith","branch":"spec-42","worktree":"` +
		evalWorkspace(t, workspace) + `/smith/worktrees/spec-42","live":false,"dirty":false,"unpushed":0,"driver":"loop"}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestSessionListJSONWithNoSessionsIsAnEmptyList(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolvedBox(workspace, "smith"), &tmuxBox{}, &fakeExec{}), "list", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := `{"schema_version":1,"sessions":[]}` + "\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestSessionListJSONRelayedCopiesTheBoxsStdoutByteForByte(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	box := "{\"schema_version\":1,  \"sessions\":[ ]}\n\n"
	ssh := &fakeSSHRelay{stdout: box}

	stdout, stderr, code := runSessionOn(t, laptop(t, dir, ssh, &fakeExec{}, &fakeRunner{}, &fakeRunner{}), "list", "dev", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != box {
		t.Errorf("stdout = %q, want the box's %q", stdout, box)
	}
	if line := ssh.line(t); !strings.Contains(line, "'--json=true'") {
		t.Errorf("ssh argv = %q, want --json passed through", line)
	}
}

func TestSessionListJSONSkewRefusalLeavesStdoutEmpty(t *testing.T) {
	ssh := &skewSSH{refuse: true}
	term := &scriptedTerminal{attended: false}

	stdout, stderr, code := runSessionOn(t, skewLaptop(t, ssh, term, &convergeRecorder{}), "list", "dev", "--json")

	if code != 6 {
		t.Fatalf("exit code = %d, want the refusal code 6", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "smith machine upgrade dev") {
		t.Errorf("stderr = %q, want the upgrade command", stderr)
	}
}

func TestSessionListJSONCarriesTheOutcomeAStoppedLoopSessionRecorded(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	standUpLoop(t, workspace, "smith", "spec-42")
	rec, err := session.OpenRecord(t.Context(), connection.System(), filepath.Join(workspace, "smith", "worktrees", "spec-42"))
	if err != nil {
		t.Fatalf("OpenRecord() err = %v", err)
	}
	if err := rec.Conclude("complete"); err != nil {
		t.Fatalf("Conclude() err = %v", err)
	}

	stdout, stderr, code := runSessionOn(t, onBoxWiring(t, resolvedBox(workspace, "smith"), &tmuxBox{}, &fakeExec{}), "list", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"sessions":[{"name":"smith-spec-42","repo":"smith","branch":"spec-42","worktree":"` +
		evalWorkspace(t, workspace) + `/smith/worktrees/spec-42","live":false,"dirty":false,"unpushed":0,"driver":"loop","outcome":"complete"}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
