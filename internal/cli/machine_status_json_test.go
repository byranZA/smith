package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/memory"
	"github.com/byranZA/smith/internal/provider"
	"github.com/byranZA/smith/internal/status"
)

func statusJSON(t *testing.T, box string, r status.Report) string {
	t.Helper()
	b, err := json.Marshal(newStatusDocument(box, r))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(b)
}

func TestStatusDocumentOfAnUnreachableBoxCarriesWhy(t *testing.T) {
	r := status.Unreachable("root@203.0.113.10", errString("ssh: connect to host 203.0.113.10 port 22: Connection timed out"))

	got := statusJSON(t, "dev", r)

	want := `{"schema_version":1,"box":"dev","verdict":"unreachable","findings":[],"error":"ssh: connect to host 203.0.113.10 port 22: Connection timed out"}`
	if got != want {
		t.Errorf("document = %s, want %s", got, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestStatusDocumentOfANeverSetUpBox(t *testing.T) {
	r := status.Reconcile(marker.Marker{}, marker.SkewNone, false, status.Facts{Memory: memory.FromKiB(4026532), Swap: memory.SwapFromKiB(0)})

	got := statusJSON(t, "dev", r)

	want := `{"schema_version":1,"box":"dev","verdict":"never_set_up","findings":[],"memory_kib":4026532,"swap_kib":0,"docker":{"ports":[],"unreadable":false}}`
	if got != want {
		t.Errorf("document = %s, want %s", got, want)
	}
}

func TestStatusDocumentOfAPartialBoxNamesTheMissingPhases(t *testing.T) {
	m := marker.Marker{AccessMode: "public", CompletedPhases: []string{"swap"}}
	r := status.Reconcile(m, marker.SkewNone, true, status.Facts{})

	doc := newStatusDocument("dev", r)

	if doc.Verdict != "partial" || len(doc.MissingPhases) == 0 || doc.MissingPhases[0] != "packages" {
		t.Errorf("verdict = %q, missing = %v, want partial missing from packages on", doc.Verdict, doc.MissingPhases)
	}
}

func okFact(v string) status.Fact { return status.Fact{Value: v, Known: true} }

func TestStatusDocumentOfADriftedBoxHoldsBothValues(t *testing.T) {
	m := marker.Marker{AccessMode: "public", CompletedPhases: bootstrap.Phases}
	facts := status.Facts{
		SmithUserExists: true, PasswordlessSudo: true,
		UFWActive: okFact("active"), UFWDefaultDeny: okFact("deny"), UFWSSHAllow: okFact("deny"),
		PermitRootLogin: okFact("no"), PasswordAuth: okFact("no"),
		Fail2banRunning: okFact("running"), AutoUpdates: okFact("enabled"),
		Docker: status.DockerPorts{
			Exposed:  []string{"web 0.0.0.0:8083->80/tcp"},
			Loopback: []string{"db 127.0.0.1:5433->5432/tcp"},
		},
	}
	r := status.Reconcile(m, marker.SkewNone, true, facts)

	doc := newStatusDocument("dev", r)

	if doc.Verdict != "drifted" || doc.AccessMode != "public" {
		t.Fatalf("verdict = %q, access = %q, want drifted public", doc.Verdict, doc.AccessMode)
	}
	want := statusFinding{Phase: "firewall", Fact: "public SSH (port 22)", Expected: "allow", Actual: "deny", Status: "drift"}
	found := false
	for _, f := range doc.Findings {
		found = found || f == want
	}
	if !found {
		t.Errorf("findings = %+v, want %+v among them", doc.Findings, want)
	}
	wantPorts := []statusPort{{"web 0.0.0.0:8083->80/tcp", true}, {"db 127.0.0.1:5433->5432/tcp", false}}
	if len(doc.Docker.Ports) != 2 || doc.Docker.Ports[0] != wantPorts[0] || doc.Docker.Ports[1] != wantPorts[1] {
		t.Errorf("ports = %+v, want %+v", doc.Docker.Ports, wantPorts)
	}
}

func runStatus(t *testing.T, dir string, exec connection.Exec, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		exec, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"status"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestMachineStatusJSONOfAnUnreachableBoxIsADocumentWithExit3(t *testing.T) {
	stdout, _, code := runStatus(t, t.TempDir(), unconnectableSSH{stderr: "ssh: connect to host 203.0.113.10 port 22: Connection timed out\n"},
		"root@203.0.113.10", "--json")

	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	var doc statusDocument
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout = %q, want one JSON document: %v", stdout, err)
	}
	if doc.Verdict != "unreachable" || doc.Box != "root@203.0.113.10" || doc.Error == "" {
		t.Errorf("document = %+v, want unreachable with the box and an error", doc)
	}
}

func TestMachineStatusJSONOfAnUnregisteredNameThatDoesNotAnswerPrintsNoDocument(t *testing.T) {
	stdout, stderr, code := runStatus(t, t.TempDir(), unconnectableSSH{}, "ghost", "--json")

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "ghost is not a registered box") {
		t.Errorf("stderr = %q, want the prose saying why", stderr)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3 as without --json", code)
	}
}
