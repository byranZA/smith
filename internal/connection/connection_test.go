package connection

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// recordingExec is a fake for the process-launch boundary. It records the last
// invocation and drains stdin so callers can assert on what was delivered.
type recordingExec struct {
	name     string
	args     []string
	stdin    string
	stdout   string // written to the caller's stdout writer
	exitCode int    // when non-zero, Run returns a fakeExit with this code
}

func (r *recordingExec) Run(_ context.Context, name string, args []string, stdin io.Reader, stdout, _ io.Writer) error {
	r.name = name
	r.args = args
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		r.stdin = string(b)
	}
	if r.stdout != "" {
		if _, err := io.WriteString(stdout, r.stdout); err != nil {
			return err
		}
	}
	if r.exitCode != 0 {
		return fakeExit{code: r.exitCode}
	}
	return nil
}

type fakeExit struct{ code int }

func (e fakeExit) Error() string { return "exit status" }
func (e fakeExit) ExitCode() int { return e.code }

func TestRunBuildsSSHCommandAndStreamsOutput(t *testing.T) {
	fake := &recordingExec{stdout: "hello from box\n"}
	c := New("root@box", fake)

	var out bytes.Buffer
	if err := c.Run(context.Background(), "uname -a", &out, io.Discard); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if fake.name != "ssh" {
		t.Errorf("launched %q, want ssh", fake.name)
	}
	if got := strings.Join(fake.args, " "); !strings.Contains(got, "root@box") || !strings.HasSuffix(got, "uname -a") {
		t.Errorf("ssh args = %q, want target root@box and trailing remote command", got)
	}
	if out.String() != "hello from box\n" {
		t.Errorf("streamed output = %q, want %q", out.String(), "hello from box\n")
	}
}

func TestRunWithInputDeliversValueOverStdinNotArgv(t *testing.T) {
	fake := &recordingExec{}
	c := New("smith@box", fake)

	secret := "tskey-abc123"
	err := c.RunWithInput(context.Background(), "cat", strings.NewReader(secret), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("RunWithInput() error = %v", err)
	}

	if fake.stdin != secret {
		t.Errorf("stdin = %q, want %q", fake.stdin, secret)
	}
	if strings.Contains(strings.Join(fake.args, " "), secret) {
		t.Errorf("secret leaked into argv %q", fake.args)
	}
}

func TestCopyBuildsScpCommand(t *testing.T) {
	fake := &recordingExec{}
	c := New("root@box", fake)

	if err := c.Copy(context.Background(), "/tmp/local.sh", "/tmp/remote.sh"); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}

	if fake.name != "scp" {
		t.Errorf("launched %q, want scp", fake.name)
	}
	got := strings.Join(fake.args, " ")
	if !strings.Contains(got, "/tmp/local.sh") || !strings.Contains(got, "root@box:/tmp/remote.sh") {
		t.Errorf("scp args = %q, want local path and root@box:/tmp/remote.sh", got)
	}
}

func TestRunClassifiesConnectFailure(t *testing.T) {
	fake := &recordingExec{exitCode: 255}
	c := New("root@unreachable", fake)

	err := c.Run(context.Background(), "true", io.Discard, io.Discard)
	if !errors.Is(err, ErrConnect) {
		t.Errorf("Run() error = %v, want wrapping ErrConnect", err)
	}
}

func TestRunNonConnectFailureIsNotConnectError(t *testing.T) {
	fake := &recordingExec{exitCode: 1}
	c := New("root@box", fake)

	err := c.Run(context.Background(), "false", io.Discard, io.Discard)
	if err == nil {
		t.Fatal("Run() error = nil, want a remote-command error")
	}
	if errors.Is(err, ErrConnect) {
		t.Errorf("Run() error = %v, should not be ErrConnect for a non-255 exit", err)
	}
}
