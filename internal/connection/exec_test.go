package connection_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

func TestSystemExecInheritsTheOperatorsEnvironment(t *testing.T) {
	const token = "hcloud-token-a1b2c3d4"
	t.Setenv("SMITH_TEST_TOKEN", token)
	var stdout, stderr bytes.Buffer

	err := connection.System().Run(context.Background(), "sh",
		[]string{"-c", `printf %s "$SMITH_TEST_TOKEN"`}, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("Run() err = %v (stderr: %s), want nil", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != token {
		t.Errorf("subprocess read %q from the environment, want the operator's own value", got)
	}
}

func TestExecReportsABinaryThatIsNotOnPath(t *testing.T) {
	t.Parallel()
	err := connection.System().Exec("smith-no-such-binary", []string{"--help"})

	if err == nil {
		t.Fatalf("Exec() err = nil, want a failure for a binary that is not on PATH")
	}
	if !strings.Contains(err.Error(), "smith-no-such-binary") {
		t.Errorf("Exec() err = %q, want it to name the binary", err)
	}
}
