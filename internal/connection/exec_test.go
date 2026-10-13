package connection_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
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

func TestSystemExecKeepsTheOperatorsLocaleOffTheBox(t *testing.T) {
	config := startSSHD(t)
	t.Setenv("LANG", "en_ZZ.UTF-8")
	t.Setenv("LC_CTYPE", "UTF-8")
	var stdout, stderr bytes.Buffer

	err := connection.System().Run(t.Context(), "ssh", []string{"-F", config, "testbox", "env"}, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("Run(ssh testbox env) err = %v (stderr: %s), want nil", err, stderr.String())
	}
	if got := localeLines(stdout.String()); len(got) != 0 {
		t.Errorf("box environment carries %q, want none of the operator's locale", got)
	}
}

func TestSystemExecKeepsTheOperatorsLocaleFromACopy(t *testing.T) {
	fakeOnPath(t, "scp", "#!/bin/sh\nenv\n")
	t.Setenv("LANG", "en_ZZ.UTF-8")
	t.Setenv("LC_CTYPE", "UTF-8")
	var stdout, stderr bytes.Buffer

	err := connection.System().Run(t.Context(), "scp", []string{"/tmp/a", "testbox:/tmp/b"}, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("Run(scp) err = %v (stderr: %s), want nil", err, stderr.String())
	}
	if got := localeLines(stdout.String()); len(got) != 0 {
		t.Errorf("scp environment carries %q, want none of the operator's locale", got)
	}
}

func TestSystemExecReplacesSmithWithSSHWithoutTheOperatorsLocale(t *testing.T) {
	if config := os.Getenv("SMITH_TEST_EXEC_SSH_CONFIG"); config != "" {
		err := connection.System().Exec("ssh", []string{"-F", config, "testbox", "env"})
		t.Fatalf("Exec(ssh) returned %v, want smith replaced", err)
	}
	config := startSSHD(t)
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSystemExecReplacesSmithWithSSHWithoutTheOperatorsLocale$")
	child.Env = append(os.Environ(), "SMITH_TEST_EXEC_SSH_CONFIG="+config, "LANG=en_ZZ.UTF-8", "LC_CTYPE=UTF-8")

	out, err := child.Output()

	if err != nil {
		t.Fatalf("replaced process err = %v (output: %s), want nil", err, out)
	}
	if got := localeLines(string(out)); len(got) != 0 {
		t.Errorf("box environment carries %q, want none of the operator's locale", got)
	}
}

func TestSystemExecHandsSSHTheOperatorsAgent(t *testing.T) {
	fakeOnPath(t, "ssh", "#!/bin/sh\nprintf %s \"$SSH_AUTH_SOCK\"\n")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("LC_CTYPE", "UTF-8")
	var stdout, stderr bytes.Buffer

	err := connection.System().Run(t.Context(), "ssh", []string{"testbox", "true"}, nil, &stdout, &stderr)

	if err != nil {
		t.Fatalf("Run(ssh) err = %v (stderr: %s), want nil", err, stderr.String())
	}
	if got := stdout.String(); got != "/tmp/agent.sock" {
		t.Errorf("ssh read SSH_AUTH_SOCK = %q, want %q", got, "/tmp/agent.sock")
	}
}
