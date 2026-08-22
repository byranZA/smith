package connection_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// TestSystemExecInheritsTheOperatorsEnvironment locks in how a provider CLI
// reaches its credential: smith never reads, resolves or stages a token, it
// launches the command inside the operator's own environment and lets the CLI
// pick the value up there itself.
func TestSystemExecInheritsTheOperatorsEnvironment(t *testing.T) {
	const token = "hcloud-token-a1b2c3d4"
	t.Setenv("SMITH_TEST_TOKEN", token)
	var stdout, stderr bytes.Buffer

	err := connection.System().Run(context.Background(), "sh",
		[]string{"-c", `printf %s "$SMITH_TEST_TOKEN"`}, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("Run() err = %v (stderr: %s)", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != token {
		t.Errorf("subprocess read %q from the environment, want the operator's own value", got)
	}
}
