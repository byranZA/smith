package loop_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/smithtool"
	"github.com/byranZA/smith/internal/tracker"
)

func TestRunInteractiveServesASocketUnderAFreshToolsDir(t *testing.T) {
	b := &board{tasks: []tracker.Task{humanTask(80)}, single: true}
	probe := &toolsProbe{next: b}
	l := loopOn(t, b, probe)
	l.ToolsDir = toolsDir(t)

	if _, err := l.RunInteractive(t.Context(), 80); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if len(probe.paths) != 1 || probe.paths[0] == "" {
		t.Errorf("SMITH_TOOLS per launch = %q, want one socket path", probe.paths)
	}
}

func TestRunInteractiveRefusesATooLongToolsDirBeforeMarkingTheTask(t *testing.T) {
	b := &board{tasks: []tracker.Task{humanTask(80)}, single: true}
	l := loopOn(t, b, b)
	l.ToolsDir = filepath.Join(t.TempDir(), strings.Repeat("d", 120), "tools")

	_, err := l.RunInteractive(t.Context(), 80)

	var tooLong *smithtool.TooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("RunInteractive() error = %v, want a too-long refusal", err)
	}
	for _, e := range b.events {
		if strings.HasPrefix(e, "start ") {
			t.Errorf("events = %q, want no task marked", b.events)
		}
	}
}

func TestRunInteractiveNotesAnEjectedPromptWithoutThePlaceholder(t *testing.T) {
	j := &journal{board: &board{tasks: []tracker.Task{humanTask(80)}, single: true}}
	l, _ := stackedJournalLoop(t, j, nil)
	l.ToolsDir = toolsDir(t)
	var progress bytes.Buffer
	l.Progress = &progress

	if _, err := l.RunInteractive(t.Context(), 80); err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}

	if got := strings.Count(progress.String(), "{{SMITH_TOOLS}} placeholder"); got != 1 {
		t.Errorf("notice count = %d in:\n%s", got, progress.String())
	}
}
