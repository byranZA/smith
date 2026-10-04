package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// fakeTracker answers `gh issue view <n> --json …` from recorded JSON keyed by
// issue number, failing as gh does for an issue it does not know, and records
// every gh command it is asked to run.
type fakeTracker struct {
	issues map[string]string
	stderr string
	ran    []string
}

func (f *fakeTracker) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.ran = append(f.ran, name+" "+strings.Join(args, " "))
	if f.stderr != "" {
		if _, err := io.WriteString(stderr, f.stderr); err != nil {
			return err
		}
		return errors.New("exit status 4")
	}
	if name != "gh" || len(args) < 3 || args[0] != "issue" || args[1] != "view" {
		return fmt.Errorf("unexpected command %s %v", name, args)
	}
	reply, ok := f.issues[args[2]]
	if !ok {
		if _, err := io.WriteString(stderr, "GraphQL: Could not resolve to an issue or pull request with the number of "+args[2]+". (repository.issue)\n"); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	_, err := io.WriteString(stdout, reply)
	return err
}

func specJSON(children ...int) string {
	var nodes []string
	for _, n := range children {
		nodes = append(nodes, fmt.Sprintf(`{"number":%d,"state":"OPEN","title":"t"}`, n))
	}
	return `{"number":42,"title":"Spec: the loop","state":"OPEN","body":"","labels":[{"name":"spec"}],"subIssues":{"nodes":[` + strings.Join(nodes, ",") + `]}}`
}

func taskJSON(n int, state, label string, blockedBy ...int) string {
	var nodes []string
	for _, b := range blockedBy {
		nodes = append(nodes, fmt.Sprintf(`{"number":%d,"state":"OPEN"}`, b))
	}
	return fmt.Sprintf(`{"number":%d,"title":"Task %d","state":%q,"body":"","labels":[{"name":%q}],"blockedBy":{"nodes":[%s]}}`, n, n, state, label, strings.Join(nodes, ","))
}

func runLoopList(t *testing.T, gh *fakeTracker, ref string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newLoopCmd(gh)
	cmd.SetArgs([]string{"list", ref})
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestLoopListNamesTheNextTaskAndWhatRemains(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44, 45, 46),
		"43": taskJSON(43, "OPEN", "ready-for-human"),
		"44": taskJSON(44, "OPEN", "ready-for-agent", 43),
		"45": taskJSON(45, "CLOSED", "ready-for-agent"),
		"46": taskJSON(46, "OPEN", "ready-for-agent"),
	}}

	stdout, stderr, code := runLoopList(t, gh, "#42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := "spec #42 Spec: the loop\n" +
		"next: #46 Task 46\n" +
		"  available: #46\n" +
		"  blocked: #44 (by #43)\n" +
		"  waiting on a human: #43\n" +
		"  closed: #45\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestLoopListSaysThereIsNoNextTaskWhenOnlyAHumanCanProceed(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-human"),
		"44": taskJSON(44, "CLOSED", "ready-for-agent"),
	}}

	stdout, _, code := runLoopList(t, gh, "42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "next: none\n") || !strings.Contains(stdout, "waiting on a human: #43\n") {
		t.Errorf("stdout = %q, want no next task and #43 waiting on a human", stdout)
	}
}

func TestLoopListReportsACompleteSpec(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "CLOSED", "ready-for-agent"),
	}}

	stdout, _, _ := runLoopList(t, gh, "https://github.com/byranZA/smith/issues/42")

	if !strings.Contains(stdout, "next: none, spec complete\n") {
		t.Errorf("stdout = %q, want the spec reported complete", stdout)
	}
}

func TestLoopListOnlyReadsTheTracker(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}

	runLoopList(t, gh, "42")

	for _, ran := range gh.ran {
		if !strings.HasPrefix(ran, "gh issue view ") {
			t.Errorf("ran %s, want only gh issue views", ran)
		}
	}
}

func TestLoopListFailsNamingTheCause(t *testing.T) {
	for name, tc := range map[string]struct {
		gh   *fakeTracker
		ref  string
		want string
	}{
		"missing issue": {&fakeTracker{issues: map[string]string{}}, "9999", "#9999 not found"},
		"not a spec":    {&fakeTracker{issues: map[string]string{"43": taskJSON(43, "OPEN", "ready-for-agent")}}, "43", "#43 is not a spec"},
		"gh unusable":   {&fakeTracker{stderr: "gh auth login\n"}, "42", "gh could not be used"},
		"not an issue":  {&fakeTracker{}, "forty-two", `"forty-two" is not an issue`},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := runLoopList(t, tc.gh, tc.ref)

			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stderr = %q, want non-zero naming %q", code, stderr, tc.want)
			}
		})
	}
}
