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

// TestExecReportsABinaryThatIsNotOnPath locks in the one path Exec can return
// on: a successful exec leaves no caller to return to, so the only answer it
// ever gives is why the replacement did not happen.
func TestExecReportsABinaryThatIsNotOnPath(t *testing.T) {
	err := connection.System().Exec("smith-no-such-binary", []string{"--help"})

	if err == nil {
		t.Fatalf("Exec() err = nil, want a failure for a binary that is not on PATH")
	}
	if !strings.Contains(err.Error(), "smith-no-such-binary") {
		t.Errorf("Exec() err = %q, want it to name the binary", err)
	}
}
