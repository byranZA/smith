package loop_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/loop"
)

func TestProcessStreamsTheAgentsOutputWithItsEnvironmentAdded(t *testing.T) {
	t.Setenv("SMITH_INHERITED", "kept")
	var out bytes.Buffer
	cmd := agent.Command{Name: "sh", Args: []string{"-c", `echo "$SMITH_INHERITED $SMITH_ADDED"`}, Env: []string{"SMITH_ADDED=added"}}

	if err := (loop.Process{Stdout: &out, Stderr: &out}).Launch(context.Background(), cmd); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	if got, want := out.String(), "kept added\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProcessReportsAnAgentThatFails(t *testing.T) {
	cmd := agent.Command{Name: "sh", Args: []string{"-c", "exit 3"}}

	if err := (loop.Process{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Launch(context.Background(), cmd); err == nil {
		t.Error("Launch() error = nil, want the failed exit reported")
	}
}

func TestProcessGivesTheTerminalsInputOnlyToAnAttachedAgent(t *testing.T) {
	tests := []struct {
		name     string
		attached bool
		want     string
	}{
		{"attached", true, "steer\n"},
		{"unattended", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			p := loop.Process{Stdin: strings.NewReader("steer\n"), Stdout: &out, Stderr: &out}

			if err := p.Launch(context.Background(), agent.Command{Name: "cat", Attached: tt.attached}); err != nil {
				t.Fatalf("Launch() error = %v", err)
			}

			if got := out.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}
