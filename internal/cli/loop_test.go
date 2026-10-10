package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/loop"
)

// fakeTracker answers `gh` from recorded JSON keyed by issue number.
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
	if name == "gh" && len(args) > 1 && args[0] == "repo" && args[1] == "view" {
		_, err := io.WriteString(stdout, `{"nameWithOwner":"byranZA/smith"}`)
		return err
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

// inRepo gives w a fresh git repo with no repo file when the test left its repo out.
func inRepo(t *testing.T, w loopWiring) loopWiring {
	t.Helper()
	if w.git == nil {
		root := t.TempDir()
		w.git = toplevelGit{root: root}
		w.workdir = func() (string, error) { return root, nil }
	}
	if w.home == nil {
		home := t.TempDir()
		w.home = func() (config.Home, error) { return config.NewHome(home), nil }
	}
	if w.pusher == nil {
		w.pusher = (&fakeOrigin{}).at
	}
	return w
}

// fakeOrigin stands in for origin, recording the directory each push was made from and failing each with err when set.
type fakeOrigin struct {
	pushedFrom []string
	err        error
}

func (f *fakeOrigin) at(dir string) loop.Pusher { return originAt{origin: f, dir: dir} }

// originAt is a push to a fakeOrigin from dir.
type originAt struct {
	origin *fakeOrigin
	dir    string
}

func (o originAt) Push(context.Context) (loop.Pushed, error) {
	o.origin.pushedFrom = append(o.origin.pushedFrom, o.dir)
	return loop.Pushed{Branch: "feat/42"}, o.origin.err
}

// defaultSettings is the settings report of a run with no flag and no repo file.
const defaultSettings = "agent:  claude (built-in default)\n" +
	"model:  the agent's own default (built-in default)\n" +
	"effort: the agent's own default (built-in default)\n" +
	"push:   on (built-in default)\n"

func runLoopList(t *testing.T, gh *fakeTracker, ref string) (stdout, stderr string, code int) {
	t.Helper()
	return runLoopListIn(t, loopWiring{gh: gh, launcher: &fakeAgent{gh: gh}, lookPath: onPath}, ref)
}

func runLoopListIn(t *testing.T, w loopWiring, ref string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newLoopCmd(inRepo(t, w))
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
	want := defaultSettings +
		"spec #42 Spec: the loop\n" +
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

func TestLoopListNamesABlockerInAnotherRepoByItsRepo(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "CLOSED", "ready-for-agent"),
		"44": `{"number":44,"title":"Task 44","state":"OPEN","body":"","labels":[{"name":"ready-for-agent"}],"blockedBy":{"nodes":[{"number":43,"state":"OPEN","url":"https://github.com/other/repo/issues/43"}]}}`,
	}}

	stdout, _, _ := runLoopList(t, gh, "42")

	if !strings.Contains(stdout, "blocked: #44 (by other/repo#43)\n") {
		t.Errorf("stdout = %q, want #44 blocked by other/repo#43", stdout)
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
		"another repo":  {&fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}, "https://github.com/other/repo/issues/42", "other/repo#42 is not in byranZA/smith"},
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

// fakeAgent is a launcher whose agent closes the task its prompt names.
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

func runLoopRun(t *testing.T, w loopWiring, ref string, flags ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newLoopCmd(inRepo(t, w))
	cmd.SetArgs(append([]string{"run", ref}, flags...))
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

	if code != 0 || len(claude.handed) != 0 || stdout != defaultSettings+"spec #42 complete\n" {
		t.Errorf("exit %d, handed %v, stdout %q; want 0, no agent run, the spec complete", code, claude.handed, stdout)
	}
}

func TestLoopRunRefusesASpecInAnotherRepoBeforeAnyAgentRuns(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	claude := &fakeAgent{gh: gh}

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "https://github.com/other/repo/issues/42")

	if code == 0 || len(claude.handed) != 0 || !strings.Contains(stderr, "other/repo#42") {
		t.Errorf("exit %d, handed %v, stderr %q; want non-zero, no agent run, naming other/repo#42", code, claude.handed, stderr)
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

	want := defaultSettings + "spec #42 stopped: no task is available for an agent\n" +
		"  blocked: #44 (by #43)\n" +
		"  waiting on a human: #43\n"
	if code == 0 || len(claude.handed) != 0 || stdout != want {
		t.Errorf("exit %d, handed %v, stdout =\n%s\nwant non-zero, no agent run, and\n%s", code, claude.handed, stdout, want)
	}
}

func TestLoopRunRetriesATaskLeftOpenThenStopsNamingItSkipped(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	idle := &idleAgent{}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idle, lookPath: onPath}, "42", "--max-attempts", "3")

	want := defaultSettings + "spec #42 stopped: the agent left a task open on every attempt\n" +
		"  skipped after 3 attempts: #43\n"
	if code == 0 || idle.runs != 3 || stdout != want {
		t.Errorf("exit %d after %d agent runs, stdout =\n%s\nwant non-zero after 3 runs, and\n%s", code, idle.runs, stdout, want)
	}
}

func TestLoopRunStopsAtTheIterationCap(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": taskJSON(44, "OPEN", "ready-for-agent"),
	}}
	idle := &idleAgent{}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idle, lookPath: onPath}, "42", "--max-iterations", "3")

	want := defaultSettings + "spec #42 stopped: reached the cap of 3 agent runs\n" +
		"  available: #44\n" +
		"  skipped after 2 attempts: #43\n"
	if code == 0 || idle.runs != 3 || stdout != want {
		t.Errorf("exit %d after %d agent runs, stdout =\n%s\nwant non-zero after 3 runs, and\n%s", code, idle.runs, stdout, want)
	}
}

func TestLoopRunRefusesLimitsBelowOne(t *testing.T) {
	for _, flag := range []string{"--max-iterations", "--max-attempts"} {
		t.Run(flag, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{}}

			_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: onPath}, "42", flag, "0")

			if code == 0 || !strings.Contains(stderr, flag) || len(gh.ran) != 0 {
				t.Errorf("exit %d, stderr %q, ran %v; want non-zero naming %s, gh never run", code, stderr, gh.ran, flag)
			}
		})
	}
}

// idleAgent is a launcher whose agent finishes without closing its task, counting its runs.
type idleAgent struct{ runs int }

func (i *idleAgent) Launch(context.Context, agent.Command) error {
	i.runs++
	return nil
}

func TestLoopRunRefusesAMissingAgentBeforeReadingTheTracker(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{}}
	missing := func(name string) (string, error) { return "", errors.New("executable file not found in $PATH") }

	_, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: &idleAgent{}, lookPath: missing}, "42")

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
		"another repo":  {&fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}, "https://github.com/other/repo/issues/42", "other/repo#42 is not in byranZA/smith"},
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

// repoWith returns wiring for a git repo whose repo file holds content, and the file's path.
func repoWith(t *testing.T, gh *fakeTracker, launcher loop.Launcher, content string) (loopWiring, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".smith", "repo.yaml")
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := loopWiring{
		gh:       gh,
		launcher: launcher,
		lookPath: onPath,
		git:      toplevelGit{root: root},
		workdir:  func() (string, error) { return root, nil },
	}
	return w, path
}

func TestLoopListReportsAnUntouchedStarterAsTheBuiltInDefaults(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "CLOSED", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &idleAgent{}, "")
	home := t.TempDir()
	if _, _, code := runRepoInit(t, rootOf(t, w), rootOf(t, w), home); code != 0 {
		t.Fatalf("repo init exit %d", code)
	}

	stdout, stderr, code := runLoopListIn(t, w, "42")

	if code != 0 || !strings.HasPrefix(stdout, defaultSettings) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and every setting from the built-in default", code, stdout, stderr)
	}
}

func rootOf(t *testing.T, w loopWiring) string {
	t.Helper()
	root, err := w.workdir()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoopRunReportsEachSettingWithItsOrigin(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, "agent: claude\nmodel: opus\n")

	stdout, stderr, code := runLoopRun(t, w, "42", "--effort", "low")

	wantReport := "agent:  claude (repo file)\n" +
		"model:  opus (repo file)\n" +
		"effort: low (flag)\n"
	wantArgs := []string{"--model", "opus", "--effort", "low"}
	if code != 0 || !strings.HasPrefix(stdout, wantReport) || len(claude.commands) != 1 || !containsRun(claude.commands[0].Args, wantArgs) {
		t.Errorf("exit %d, stdout %q, stderr %q, ran %v; want 0, the report\n%s\nand claude given %q", code, stdout, stderr, claude.commands, wantReport, wantArgs)
	}
}

func TestLoopRunFlagOverridesTheRepoFileForOneRunAndLeavesItUnchanged(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	content := "model: opus\n"
	w, path := repoWith(t, gh, claude, content)

	stdout, _, code := runLoopRun(t, w, "42", "--model", "sonnet")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || !strings.Contains(stdout, "model:  sonnet (flag)\n") || len(claude.commands) != 1 || !containsRun(claude.commands[0].Args, []string{"--model", "sonnet"}) || string(after) != content {
		t.Errorf("exit %d, stdout %q, ran %v, repo file %q; want sonnet from a flag and the repo file unchanged", code, stdout, claude.commands, after)
	}
}

func TestLoopRunWithNoModelGivesTheAgentNone(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, "")

	_, _, code := runLoopRun(t, w, "42")

	if code != 0 || len(claude.commands) != 1 || slices.Contains(claude.commands[0].Args, "--model") || slices.Contains(claude.commands[0].Args, "--effort") {
		t.Errorf("exit %d, ran %v; want claude given no model and no effort", code, claude.commands)
	}
}

func TestLoopRunRefusesInvalidSettingsBeforeAnyAgentRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		repoFile string
		flags    []string
		want     []string
	}{
		"unknown key":      {"agent: claude\n\nmodle: opus\n", nil, []string{"modle", "line 3"}},
		"unknown agent":    {"agent: gemini\n", nil, []string{`"gemini"`, "known agents are claude, codex, pi"}},
		"effort off scale": {"", []string{"--effort", "extreme"}, []string{`"extreme"`, "low, medium, high"}},
	} {
		t.Run(name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
			idle := &idleAgent{}
			w, _ := repoWith(t, gh, idle, tc.repoFile)

			_, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			named := true
			for _, want := range tc.want {
				named = named && strings.Contains(stderr, want)
			}
			if code == 0 || !named || idle.runs != 0 || len(gh.ran) != 0 {
				t.Errorf("exit %d, stderr %q, %d agent runs, gh ran %v; want non-zero naming %q, no agent and no gh", code, stderr, idle.runs, gh.ran, tc.want)
			}
		})
	}
}

// recordingAgent is a fakeAgent that also records every command it was handed.
type recordingAgent struct {
	fakeAgent
	commands []agent.Command
}

func (r *recordingAgent) Launch(ctx context.Context, cmd agent.Command) error {
	r.commands = append(r.commands, cmd)
	return r.fakeAgent.Launch(ctx, cmd)
}

// containsRun reports whether args holds want as a contiguous run.
func containsRun(args, want []string) bool {
	for i := range args {
		if slices.Equal(args[i:min(i+len(want), len(args))], want) {
			return true
		}
	}
	return false
}

func TestLoopRunInteractiveAttachesTheAgentForOneTaskAndSucceedsWhenItIsClosed(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43, 44),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
		"44": taskJSON(44, "OPEN", "ready-for-agent"),
	}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}

	stdout, stderr, code := runLoopRun(t, loopWiring{gh: gh, launcher: claude, lookPath: onPath}, "42", "--interactive")

	attached := len(claude.commands) == 1 && claude.commands[0].Attached &&
		strings.Contains(claude.commands[0].Args[len(claude.commands[0].Args)-1], "A human is at the terminal")
	if code != 0 || !slices.Equal(claude.handed, []string{"43"}) || !attached || stdout != defaultSettings+"task #43 closed\n" {
		t.Errorf("exit %d, handed %v, commands %+v, stdout %q, stderr %q; want 0, #43 alone to an attached agent with the interactive note, and #43 reported closed", code, claude.handed, claude.commands, stdout, stderr)
	}
}

func TestLoopRunInteractiveFailsWhenTheTaskIsLeftOpen(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{
		"42": specJSON(43),
		"43": taskJSON(43, "OPEN", "ready-for-agent"),
	}}
	idle := &idleAgent{}

	stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idle, lookPath: onPath}, "42", "--interactive")

	if code == 0 || idle.runs != 1 || stdout != defaultSettings+"task #43 still open\n" {
		t.Errorf("exit %d after %d agent runs, stdout %q; want non-zero after 1 run, reporting #43 still open", code, idle.runs, stdout)
	}
}

func TestLoopRunInteractiveWithNothingAvailableRunsNoAgentAndSaysWhy(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		wantCode int
		want     string
	}{
		{"spec complete", taskJSON(43, "CLOSED", "ready-for-agent"), 0, "spec #42 complete\n"},
		{"only a human task", taskJSON(43, "OPEN", "ready-for-human"), 1, "spec #42 stopped: no task is available for an agent\n  waiting on a human: #43\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": tt.task}}
			idle := &idleAgent{}

			stdout, _, code := runLoopRun(t, loopWiring{gh: gh, launcher: idle, lookPath: onPath}, "42", "--interactive")

			if code != tt.wantCode || idle.runs != 0 || stdout != defaultSettings+tt.want {
				t.Errorf("exit %d after %d agent runs, stdout %q; want %d, no agent run, and %q", code, idle.runs, stdout, tt.wantCode, tt.want)
			}
		})
	}
}

// ejectPrompt writes content as w's ejected loop prompt and returns its path.
func ejectPrompt(t *testing.T, w loopWiring, content string) string {
	t.Helper()
	path := filepath.Join(rootOf(t, w), ".smith", "prompt.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoopRunHandsTheAgentTheEjectedPromptAndLeavesItUnchanged(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
	w, _ := repoWith(t, gh, claude, "")
	content := "Work issue **#{{TASK_NUMBER}} ({{TASK_TITLE}}). Run make check before closing.\n"
	path := ejectPrompt(t, w, content)

	_, stderr, code := runLoopRun(t, w, "42")

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "Work issue **#43 (Task 43). Run make check before closing.\n"
	if code != 0 || len(claude.commands) != 1 || claude.commands[0].Args[len(claude.commands[0].Args)-1] != want || string(after) != content {
		t.Errorf("exit %d, stderr %q, ran %v, prompt file %q; want the agent handed %q and the prompt file unchanged", code, stderr, claude.commands, after, want)
	}
}

func TestLoopRunRefusesAnUnknownPlaceholderBeforeAnyAgentRuns(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	idle := &idleAgent{}
	w, _ := repoWith(t, gh, idle, "")
	ejectPrompt(t, w, "Do #{{TASK_NUMBER}}, see {{ISSUE_URL}}.\n")

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || !strings.Contains(stderr, "{{ISSUE_URL}}") || !strings.Contains(stderr, "known placeholders are {{TASK_NUMBER}}, {{TASK_TITLE}}\n") || idle.runs != 0 || len(gh.ran) != 0 {
		t.Errorf("exit %d, stderr %q, %d agent runs, gh ran %v; want non-zero naming the placeholder and the known ones, no agent and no gh", code, stderr, idle.runs, gh.ran)
	}
}

func runLoopPrompt(t *testing.T, w loopWiring) (stdout string, code int) {
	t.Helper()
	cmd := newLoopCmd(inRepo(t, w))
	cmd.SetArgs([]string{"prompt"})
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), code
}

func TestLoopPromptPrintsTheBuiltInUnfilledEvenWhenTheRepoHasEjected(t *testing.T) {
	w, _ := repoWith(t, &fakeTracker{}, &idleAgent{}, "")
	ejectPrompt(t, w, "Our own prompt for #{{TASK_NUMBER}}.\n")

	stdout, code := runLoopPrompt(t, w)

	if code != 0 || !strings.HasPrefix(stdout, "You are one iteration of a smith loop.") || !strings.Contains(stdout, "**#{{TASK_NUMBER}} — {{TASK_TITLE}}**") {
		t.Errorf("exit %d, stdout %q; want 0 and the built-in prompt with its placeholders unfilled", code, stdout)
	}
}

func TestSavingThePrintedPromptAsTheEjectedPromptHandsTheAgentWhatTheBuiltInDid(t *testing.T) {
	handed := func(eject bool) []string {
		gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
		claude := &recordingAgent{fakeAgent: fakeAgent{gh: gh}}
		w, _ := repoWith(t, gh, claude, "")
		if eject {
			printed, code := runLoopPrompt(t, w)
			if code != 0 {
				t.Fatalf("loop prompt exit %d", code)
			}
			ejectPrompt(t, w, printed)
		}
		if _, stderr, code := runLoopRun(t, w, "42"); code != 0 || len(claude.commands) != 1 {
			t.Fatalf("loop run exit %d, stderr %q, ran %v", code, stderr, claude.commands)
		}
		return claude.commands[0].Args
	}

	if builtin, ejected := handed(false), handed(true); !slices.Equal(builtin, ejected) {
		t.Errorf("ejected copy handed the agent\n%q\nwant what the built-in did\n%q", ejected, builtin)
	}
}

func TestRepoInitAndTheLoopLeaveAnEjectedPromptByteForByteUnchanged(t *testing.T) {
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &recordingAgent{fakeAgent: fakeAgent{gh: gh}}, "")
	content := "Our own prompt: work issue **#{{TASK_NUMBER}} carefully.\n\n"
	path := ejectPrompt(t, w, content)

	if _, stderr, code := runRepoInit(t, rootOf(t, w), rootOf(t, w), t.TempDir()); code != 0 {
		t.Fatalf("repo init exit %d, stderr %q", code, stderr)
	}
	if _, stderr, code := runLoopRun(t, w, "42"); code != 0 {
		t.Fatalf("loop run exit %d, stderr %q", code, stderr)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != content {
		t.Errorf("prompt file = %q, want it unchanged as %q", after, content)
	}
}

func TestLoopListReportsThePushSettingWithItsOrigin(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "CLOSED", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &idleAgent{}, "push: false\n")

	stdout, stderr, code := runLoopListIn(t, w, "42")

	if code != 0 || !strings.Contains(stdout, "push:   off (repo file)\n") {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and push off from the repo file", code, stdout, stderr)
	}
}

func TestLoopRunPushesFromTheRepoAfterEachClosedTaskByDefault(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	origin := &fakeOrigin{}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "")
	w.pusher = origin.at

	_, stderr, code := runLoopRun(t, w, "42")

	root := rootOf(t, w)
	if want := []string{root, root}; code != 0 || !slices.Equal(origin.pushedFrom, want) {
		t.Errorf("exit %d, stderr %q, pushed from %q; want 0 and a push from %q after each of #43 and #44", code, stderr, origin.pushedFrom, root)
	}
}

func TestLoopRunPushesNothingWhenPushIsOff(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		repoFile string
		flags    []string
		want     string
	}{
		"in the repo file":                  {"push: false\n", nil, "push:   off (repo file)\n"},
		"by --no-push over the repo file":   {"push: true\n", []string{"--no-push"}, "push:   off (flag)\n"},
		"by --no-push with no repo setting": {"", []string{"--no-push"}, "push:   off (flag)\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
			origin := &fakeOrigin{}
			w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, tc.repoFile)
			w.pusher = origin.at

			stdout, stderr, code := runLoopRun(t, w, "42", tc.flags...)

			if code != 0 || len(origin.pushedFrom) != 0 || !strings.Contains(stdout, tc.want) {
				t.Errorf("exit %d, stdout %q, stderr %q, pushed from %q; want 0, no push and %q", code, stdout, stderr, origin.pushedFrom, tc.want)
			}
		})
	}
}

func TestLoopRunStopsOnAFailedPushNamingTheTaskAndTheError(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	claude := &fakeAgent{gh: gh}
	w, _ := repoWith(t, gh, claude, "")
	w.pusher = (&fakeOrigin{err: errors.New("remote rejected")}).at

	_, stderr, code := runLoopRun(t, w, "42")

	if code == 0 || !strings.Contains(stderr, "push the work on #43: remote rejected") || !slices.Equal(claude.handed, []string{"43"}) {
		t.Errorf("exit %d, stderr %q, handed %v; want non-zero naming #43 and the push error, with #44 never handed", code, stderr, claude.handed)
	}
}
