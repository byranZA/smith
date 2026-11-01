package loop_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/tracker"
)

// placing fakes the session placer, logging each worktree it places into among board's events.
type placing struct {
	board *board
	fail  error
}

func (p placing) Place(repo, dir string) error {
	p.board.events = append(p.board.events, "place "+repo+" "+dir)
	return p.fail
}

// ignoring fakes git check-ignore: it prints the paths it was asked about that are in ignored, and exits 1 when there are none.
type ignoring struct{ ignored []string }

func (g ignoring) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	var out []string
	for _, a := range args[slices.Index(args, "--")+1:] {
		if slices.Contains(g.ignored, a) {
			out = append(out, a)
		}
	}
	_, _ = io.WriteString(stdout, strings.Join(out, "\n"))
	if len(out) == 0 {
		return errors.New("exit status 1")
	}
	return nil
}

func TestRunPlacesTheReposFilesBeforePreparationAndTheAgent(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Preparer = preparing{board: b}
	l.Placements = &loop.Placements{Repo: "api", To: []string{".env"}, Placer: placing{board: b}, Git: ignoring{ignored: []string{".env"}}}

	if _, err := l.Run(t.Context(), 42); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []string{"place api /bench/43", "prepare /bench/43", "start 43", "launch 43", "stop 43"}
	if !slices.Equal(b.events[:5], want) {
		t.Errorf("Run() events = %q, want it to begin %q", b.events, want)
	}
}

func TestRunRefusesAPlacementDestinationThatIsNotGitignoredBeforeMarkingAnyTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Placements = &loop.Placements{Repo: "api", To: []string{".env", "config/local.yml"}, Placer: placing{board: b}, Git: ignoring{ignored: []string{".env"}}}

	_, err := l.Run(t.Context(), 42)

	if err == nil || !strings.Contains(err.Error(), "config/local.yml") || !strings.Contains(err.Error(), ".gitignore") || strings.Contains(err.Error(), ".env,") {
		t.Fatalf("Run() error = %v, want one naming config/local.yml and .gitignore", err)
	}
	if len(b.events) != 0 || len(b.launched) != 0 {
		t.Errorf("Run() events = %q, launched %d, want nothing placed, marked or launched", b.events, len(b.launched))
	}
}

func TestRunStopsBeforeAnyAgentWhenAPlacementFails(t *testing.T) {
	errPlace := errors.New("no staged bytes")
	b := &board{tasks: []tracker.Task{agentTask(43)}}
	l := loopOn(t, b, b)
	l.Placements = &loop.Placements{Repo: "api", To: []string{".env"}, Placer: placing{board: b, fail: errPlace}, Git: ignoring{ignored: []string{".env"}}}

	_, err := l.Run(t.Context(), 42)

	if !errors.Is(err, errPlace) || !strings.Contains(err.Error(), "#43") || len(b.launched) != 0 {
		t.Errorf("Run() = %v after %d agent runs, want the placement error naming #43 and no agent run", err, len(b.launched))
	}
}
