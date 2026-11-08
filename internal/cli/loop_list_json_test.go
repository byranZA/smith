package cli

import (
	"strings"
	"testing"
)

func TestLoopListJSONPrintsTheWholeDocumentInSpecOrder(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44, 45, 46, 47),
		"43": taskJSON(43, "OPEN", "ready-for-human"),
		"44": `{"number":44,"title":"Task 44","state":"OPEN","body":"","labels":[{"name":"ready-for-agent"}],"blockedBy":{"nodes":[{"number":43,"state":"OPEN"},{"number":45,"state":"CLOSED"}]}}`,
		"45": taskJSON(45, "CLOSED", "ready-for-agent"),
		"46": taskJSON(46, "OPEN", "ready-for-agent"),
		"47": `{"number":47,"title":"Task 47","state":"OPEN","body":"","labels":[],"blockedBy":{"nodes":[]}}`,
	}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, "#42", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := `{"schema_version":1,"spec":42,"work_branch":"smith/42-the-loop","next":46,"tasks":[` +
		`{"number":43,"title":"Task 43","labels":["ready-for-human"],"state":"human","blocked_by":[]},` +
		`{"number":44,"title":"Task 44","labels":["ready-for-agent"],"state":"blocked","blocked_by":[43]},` +
		`{"number":45,"title":"Task 45","labels":["ready-for-agent"],"state":"closed","blocked_by":[]},` +
		`{"number":46,"title":"Task 46","labels":["ready-for-agent"],"state":"available","blocked_by":[]},` +
		`{"number":47,"title":"Task 47","labels":[],"state":"unready","blocked_by":[]}]}` + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestLoopListJSONWithNothingAvailableHasANullNext(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-human"),
	}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, "#42", "--json")

	if want := `"next":null,`; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit code = %d, stdout = %s, want 0 and %s (stderr: %s)", code, stdout, want, stderr)
	}
}

func TestLoopListJSONOfAnUnreachableIssuePrintsNoDocument(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{}}

	stdout, stderr, code := runLoopListIn(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, "9999", "--json")

	if code == 0 || stdout != "" || !strings.Contains(stderr, "#9999 not found") {
		t.Errorf("exit code = %d, stdout = %q, stderr = %q, want a failure, empty stdout and the reason", code, stdout, stderr)
	}
}
