package loop_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/prepare"
	"github.com/byranZA/smith/internal/smithtool"
	"github.com/byranZA/smith/internal/stack"
	"github.com/byranZA/smith/internal/tracker"
)

// toolsProbe wraps a launcher and, for each launch, records the SMITH_TOOLS socket the agent was given and what listing it returned while the agent ran.
type toolsProbe struct {
	next    loop.Launcher
	paths   []string
	listing [][]smithtool.Info
	errs    []error
}

func (p *toolsProbe) Launch(ctx context.Context, cmd agent.Command) error {
	path := ""
	for _, kv := range cmd.Env {
		if v, ok := strings.CutPrefix(kv, loop.ToolsEnv+"="); ok {
			path = v
		}
	}
	p.paths = append(p.paths, path)
	if path != "" {
		infos, err := smithtool.List(ctx, path)
		p.listing = append(p.listing, infos)
		p.errs = append(p.errs, err)
	}
	return p.next.Launch(ctx, cmd)
}

// toolsDir returns a tools directory path short enough for sockets.
func toolsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "tools")
}

func TestRunServesEachTaskAttemptASocketInSmithTools(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43), agentTask(44)}}
	probe := &toolsProbe{next: b}
	l := loopOn(t, b, probe)
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(probe.paths) != 2 || probe.paths[0] == "" || probe.paths[0] == probe.paths[1] {
		t.Fatalf("SMITH_TOOLS per launch = %q, want two different socket paths", probe.paths)
	}
	if want := [][]smithtool.Info{{}, {}}; !reflect.DeepEqual(probe.listing, want) || !reflect.DeepEqual(probe.errs, []error{nil, nil}) {
		t.Errorf("listings = %v, errors = %v; want an empty listing each", probe.listing, probe.errs)
	}
}

func TestRunClosesTheSocketWhenTheAgentExits(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	probe := &toolsProbe{next: b}
	l := loopOn(t, b, probe)
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	_, err := smithtool.List(t.Context(), probe.paths[0])
	var refused *smithtool.RefusedError
	if !errors.As(err, &refused) {
		t.Errorf("List on the socket after the agent exited = %v, want a refusal", err)
	}
}

func TestRunWithNoToolsDirGivesTheAgentNoSmithTools(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	probe := &toolsProbe{next: b}

	if _, err := loopOn(t, b, probe).Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if want := []string{""}; !slices.Equal(probe.paths, want) {
		t.Errorf("SMITH_TOOLS per launch = %q, want %q", probe.paths, want)
	}
}

func TestRunServesTheReviewASocketToo(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	probe := &toolsProbe{next: launcher}
	l.Launcher = probe
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(probe.paths) != 1 || probe.paths[0] == "" {
		t.Errorf("SMITH_TOOLS for the review = %q, want one socket path", probe.paths)
	}
}

func TestRunRefusesATooLongToolsDirBeforeMarkingAnyTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.ToolsDir = filepath.Join(t.TempDir(), strings.Repeat("d", 120), "tools")

	_, err := l.Run(t.Context(), 42)

	var tooLong *smithtool.TooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("Run() error = %v, want a too-long refusal", err)
	}
	if len(b.handed()) != 0 {
		t.Errorf("handed %v, want no task", b.handed())
	}
	for _, e := range b.events {
		if strings.HasPrefix(e, "start ") {
			t.Errorf("events = %q, want no task marked", b.events)
		}
	}
}

// callingLauncher wraps a launcher and, for each launch, calls the smith tool name over the SMITH_TOOLS socket while the agent runs, keeping its output and error.
type callingLauncher struct {
	next    loop.Launcher
	name    string
	output  []string
	errs    []error
	listing [][]smithtool.Info
}

func (c *callingLauncher) Launch(ctx context.Context, cmd agent.Command) error {
	for _, kv := range cmd.Env {
		if path, ok := strings.CutPrefix(kv, loop.ToolsEnv+"="); ok {
			var out strings.Builder
			c.errs = append(c.errs, smithtool.Call(ctx, path, c.name, nil, &out))
			c.output = append(c.output, out.String())
			infos, _ := smithtool.List(ctx, path)
			c.listing = append(c.listing, infos)
		}
	}
	return c.next.Launch(ctx, cmd)
}

// failSecondUp fails every up after the first, as a stack that came up and then broke.
type failSecondUp struct {
	stackCommands
	ups int
}

func (f *failSecondUp) Exec(ctx context.Context, dir, step, command string, out io.Writer) error {
	if err := f.stackCommands.Exec(ctx, dir, step, command, out); err != nil {
		return err
	}
	if step == stack.Up {
		f.ups++
		if f.ups > 1 {
			return &prepare.StepError{Step: step, Status: 7, Command: command}
		}
	}
	return nil
}

func TestStackUpToolRerunsTheRecordedUpAndStreamsItsOutput(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, d := stackedJournalLoop(t, j, nil)
	call := &callingLauncher{next: l.Launcher, name: "stack-up"}
	l.Launcher = call
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !reflect.DeepEqual(call.errs, []error{nil}) || call.output[0] != "pulling images\n" {
		t.Errorf("stack-up error = %v, output = %q; want success streaming %q", call.errs, call.output, "pulling images\n")
	}
	if got := countPrefix(j.events, "up "+d); got != 2 {
		t.Errorf("events = %q, want the up run twice", j.events)
	}
	if got := countPrefix(j.events, "down "); got != 1 {
		t.Errorf("events = %q, want one down", j.events)
	}
	if want := []smithtool.Info{{Name: "stack-up", Summary: "re-run the stack's recorded up in this worktree"}}; !reflect.DeepEqual(call.listing[0], want) {
		t.Errorf("listing = %v, want %v", call.listing[0], want)
	}
}

func TestAFailedStackUpToolReportsUpsStatusAndLeavesTheAttemptAlone(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{agentTask(43)}}}
	l, d := stackedJournalLoop(t, j, nil)
	l.Stack = &stack.Stack{Command: &failSecondUp{stackCommands: stackCommands{events: &j.events}}}
	call := &callingLauncher{next: l.Launcher, name: "stack-up"}
	l.Launcher = call
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v, want the attempt to go on", err)
	}

	var failed *smithtool.FailedError
	if !errors.As(call.errs[0], &failed) || !strings.Contains(failed.Error(), "status 7") {
		t.Errorf("stack-up error = %v, want a failure naming status 7", call.errs[0])
	}
	if !slices.Contains(j.events, "land 43") || !slices.Contains(j.events, "down "+d) {
		t.Errorf("events = %q, want the task landed and the stack down", j.events)
	}
}

func TestStackUpToolIsRefusedWhenSmithRunsNoStack(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	call := &callingLauncher{next: b, name: "stack-up"}
	l := loopOn(t, b, call)
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var refused *smithtool.RefusedError
	if !errors.As(call.errs[0], &refused) || !strings.Contains(refused.Error(), "smith runs no stack in this run") {
		t.Errorf("stack-up error = %v, want a refusal saying smith runs no stack in this run", call.errs[0])
	}
	if len(call.listing[0]) != 0 {
		t.Errorf("listing = %v, want no tools", call.listing[0])
	}
}

func TestReviewAgentCanReupTheStack(t *testing.T) {
	b := &board{tasks: []tracker.Task{closed(agentTask(43))}}
	l, launcher := reviewingLoop(t, b, noFindings)
	dir := repoFileIn(t, t.TempDir())
	l.Bench = reviewTree{reviewBench: l.Bench.(reviewBench), dir: dir}
	l.Stack = &stack.Stack{Command: stackCommands{events: &b.events}}
	call := &callingLauncher{next: launcher, name: "stack-up"}
	l.Launcher = call
	l.ToolsDir = toolsDir(t)

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !reflect.DeepEqual(call.errs, []error{nil}) || countPrefix(b.events, "up "+dir) != 2 {
		t.Errorf("stack-up errors = %v, events = %q; want success and the up run twice", call.errs, b.events)
	}
}
