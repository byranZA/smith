package loop_test

import (
	"bytes"
	"context"
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
