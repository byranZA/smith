package loop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/smithtool"
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
