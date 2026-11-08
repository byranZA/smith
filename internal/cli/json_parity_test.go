package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
)

func TestMachineStatusExitCodeIsTheSameWithAndWithoutJSON(t *testing.T) {
	allPhases := `["` + strings.Join(bootstrap.Phases, `","`) + `"]`
	tests := []struct {
		name     string
		exec     connection.Exec
		wantCode int
	}{
		{"matches", probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":`+allPhases+`}`, "allow")}, 0},
		{"drifted", probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":`+allPhases+`}`, "deny")}, 1},
		{"partial", probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":["swap"]}`, "allow")}, 2},
		{"never set up", probeBoxSSH{"mem-total-kb=4026532\nswap-total-kb=0\n"}, 2},
		{"unreachable", unconnectableSSH{stderr: "ssh: connect to host 203.0.113.10 port 22: Connection timed out\n"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, prose := runStatus(t, t.TempDir(), tt.exec, "root@203.0.113.10")
			_, _, asJSON := runStatus(t, t.TempDir(), tt.exec, "root@203.0.113.10", "--json")

			if prose != tt.wantCode || asJSON != tt.wantCode {
				t.Errorf("exit codes without and with --json = %d and %d, want both %d", prose, asJSON, tt.wantCode)
			}
		})
	}
}

func TestMachineStatusJSONWithAnExposedDockerPortKeepsStdoutOneObjectAndStderrEmpty(t *testing.T) {
	allPhases := `["` + strings.Join(bootstrap.Phases, `","`) + `"]`
	exec := probeBoxSSH{probeOutputWithMarker(`{"schema_version":2,"access_mode":"public","completed_phases":`+allPhases+`}`, "allow")}

	stdout, stderr, _ := runStatus(t, t.TempDir(), exec, "root@203.0.113.10", "--json")

	assertSingleObject(t, stdout)
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"exposed":true`) {
		t.Errorf("stdout = %s, want the exposed port in the document", stdout)
	}
}

func TestBlueprintCheckExitCodeIsTheSameWithAndWithoutJSON(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"valid blueprint", "terminal: tmux\n", 0},
		{"blueprint with a problem", "terminals: tmux\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeBlueprint(t, dir, "acme", tt.body)

			_, _, prose := runCheck(t, dir, "check", "acme")
			_, _, asJSON := runCheck(t, dir, "check", "acme", "--json")

			if prose != tt.wantCode || asJSON != tt.wantCode {
				t.Errorf("exit codes without and with --json = %d and %d, want both %d", prose, asJSON, tt.wantCode)
			}
		})
	}
}

func TestMachineListExitCodeIsTheSameWithAndWithoutJSON(t *testing.T) {
	tests := []struct {
		name      string
		inventory string
		wantCode  int
	}{
		{"readable inventory", twoBoxInventory, 0},
		{"unreadable inventory", "{not json", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeInventory(t, dir, tt.inventory)

			_, _, prose := runList(t, dir)
			_, _, asJSON := runList(t, dir, "--json")

			if prose != tt.wantCode || asJSON != tt.wantCode {
				t.Errorf("exit codes without and with --json = %d and %d, want both %d", prose, asJSON, tt.wantCode)
			}
		})
	}
}

func TestMachineListJSONWithANewerSmithInventoryKeepsStdoutOneObject(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":99,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	stdout, stderr, _ := runList(t, dir, "--json")

	assertSingleObject(t, stdout)
	if !strings.Contains(stderr, "newer smith") {
		t.Errorf("stderr = %q, want the warning", stderr)
	}
}

func TestLoopListExitCodeIsTheSameWithAndWithoutJSON(t *testing.T) {
	tests := []struct {
		name   string
		issues map[string]string
		ref    string
	}{
		{"readable spec", map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}, "#42"},
		{"unreachable issue", map[string]string{}, "9999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := &fakeTracker{issues: tt.issues}
			w := loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}

			_, _, prose := runLoopListIn(t, w, tt.ref)
			_, _, asJSON := runLoopListIn(t, w, tt.ref, "--json")

			if prose != asJSON {
				t.Errorf("exit codes without and with --json = %d and %d, want them equal", prose, asJSON)
			}
		})
	}
}

func TestSessionListExitCodeIsTheSameWithAndWithoutJSON(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &tmuxBox{}, &fakeExec{})

	_, _, prose := runSessionOn(t, w, "list")
	_, _, asJSON := runSessionOn(t, w, "list", "--json")

	if prose != 0 || asJSON != 0 {
		t.Errorf("exit codes without and with --json = %d and %d, want both 0", prose, asJSON)
	}
}

func assertSingleObject(t *testing.T, stdout string) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(stdout)))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("stdout = %q does not parse as an object: %v", stdout, err)
	}
	if dec.More() {
		t.Errorf("stdout = %q holds more than one document", stdout)
	}
}
