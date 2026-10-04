package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
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
	cmd := newLoopCmd(loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath})
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

// onPath finds every program, as a machine with all agents installed would.
func onPath(name string) (string, error) { return "/usr/local/bin/" + name, nil }

// fakeAgent is a launcher whose agent closes, in the fake tracker, the task its
// prompt names, recording each task it was handed.
type fakeAgent struct {
	gh     *fakeTracker
	handed []string
}

func (f *fakeAgent) Launch(_ context.Context, cmd agent.Command) error {
	prompt := cmd.Args[len(cmd.Args)-1]
	for n, issue := range f.gh.issues {
		if n != "42" && strings.Contains(prompt, "issue **#"+n+" ") {
			f.handed = append(f.handed, n)
			f.gh.issues[n] = strings.Replace(issue, `"state":"OPEN"`, `"state":"CLOSED"`, 1)
		}
	}
	return nil
}

func runLoopRun(t *testing.T, w loopWiring, ref string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newLoopCmd(w)
	cmd.SetArgs([]string{"run", ref})
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestLoopRunWorksEachTaskInOrderAndReportsTheSpecComplete(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": taskJSON(44, "OPEN", "ready-for-agent"),
	}}
	claude := &fakeAgent{gh: gh}

	stdout, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "42")

	if code != 0 || !slices.Equal(claude.handed, []string{"43", "44"}) || !strings.HasSuffix(stdout, "spec #42 complete\n") {
		t.Errorf("exit %d, handed %v, stdout %q, stderr %q; want 0, #43 then #44, and the spec complete", code, claude.handed, stdout, stderr)
	}
}

func TestLoopRunOnACompleteSpecRunsNoAgentAndSucceeds(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "CLOSED", "ready-for-agent"),
	}}
	claude := &fakeAgent{gh: gh}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "#42")

	if code != 0 || len(claude.handed) != 0 || stdout != "spec #42 complete\n" {
		t.Errorf("exit %d, handed %v, stdout %q; want 0, no agent run, the spec complete", code, claude.handed, stdout)
	}
}

func TestLoopRunStopsNamingTheHumanAndBlockedTasksLeft(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-human"),
		"44": taskJSON(44, "OPEN", "ready-for-agent", 43),
	}}
	claude := &fakeAgent{gh: gh}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "42")

	want := "spec #42 stopped: no task is available for an agent\n" +
		"  blocked: #44 (by #43)\n" +
		"  waiting on a human: #43\n"
	if code == 0 || len(claude.handed) != 0 || stdout != want {
		t.Errorf("exit %d, handed %v, stdout =\n%s\nwant non-zero, no agent run, and\n%s", code, claude.handed, stdout, want)
	}
}

func TestLoopRunStopsNamingATaskTheAgentLeftOpen(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idleAgent{}, lookPath: onPath}, "42")

	if code == 0 || !strings.Contains(stdout, "spec #42 stopped: the agent left #43 open\n") {
		t.Errorf("exit %d, stdout %q; want non-zero naming #43 as left open", code, stdout)
	}
}

// idleAgent is a launcher whose agent finishes without closing its task.
type idleAgent struct{}

func (idleAgent) Launch(context.Context, agent.Command) error { return nil }

func TestLoopRunRefusesAMissingAgentBeforeReadingTheTracker(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{}}
	missing := func(name string) (string, error) { return "", errors.New("executable file not found in $PATH") }

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: idleAgent{}, lookPath: missing}, "42")

	if code == 0 || !strings.Contains(stderr, "claude") || len(gh.ran) != 0 {
		t.Errorf("exit %d, stderr %q, ran %v; want non-zero naming claude, gh never run", code, stderr, gh.ran)
	}
}

func TestLoopRunFailsNamingTheCauseBeforeAnyAgentRuns(t *testing.T) {
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
			claude := &fakeAgent{gh: tc.gh}

			_, stderr, code := runLoopRun(t, loopWiring{gh: tc.gh, launcher: claude, lookPath: onPath}, tc.ref)

			if code == 0 || !strings.Contains(stderr, tc.want) || len(claude.handed) != 0 {
				t.Errorf("exit %d, stderr = %q, handed %v; want non-zero naming %q, no agent run", code, stderr, claude.handed, tc.want)
			}
		})
	}
}
