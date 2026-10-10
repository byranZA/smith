package connection

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// recordingExec records the last launch and plays back canned output and exit code.
type recordingExec struct {
	name        string
	args        []string
	stdin       string
	replyStdout string
	replyStderr string
	exitCode    int
}

func (r *recordingExec) Run(_ context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	r.name = name
	r.args = args
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		r.stdin = string(b)
	}
	if r.replyStdout != "" {
		if _, err := io.WriteString(stdout, r.replyStdout); err != nil {
			return err
		}
	}
	if r.replyStderr != "" {
		if _, err := io.WriteString(stderr, r.replyStderr); err != nil {
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

func TestRunLaunchesTheRemoteCommandOverSSH(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{}
	c := New("root@box", fake)

	if err := c.Run(context.Background(), "uname -a", io.Discard, io.Discard); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got := fake.name + " " + strings.Join(fake.args, " "); !strings.HasPrefix(got, "ssh ") || !strings.Contains(got, "root@box") || !strings.HasSuffix(got, "uname -a") {
		t.Errorf("launched %q, want ssh with target root@box and trailing remote command", got)
	}
}

func TestRunStreamsTheRemoteOutput(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{replyStdout: "hello from box\n"}
	c := New("root@box", fake)

	var out bytes.Buffer
	if err := c.Run(context.Background(), "uname -a", &out, io.Discard); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if out.String() != "hello from box\n" {
		t.Errorf("streamed output = %q, want %q", out.String(), "hello from box\n")
	}
}

func TestRunWithInputDeliversValueOverStdin(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{}
	c := New("smith@box", fake)

	if err := c.RunWithInput(context.Background(), "cat", strings.NewReader("tskey-abc123"), io.Discard, io.Discard); err != nil {
		t.Fatalf("RunWithInput() error = %v, want nil", err)
	}

	if fake.stdin != "tskey-abc123" {
		t.Errorf("stdin = %q, want %q", fake.stdin, "tskey-abc123")
	}
}

func TestRunWithInputKeepsValueOutOfArgv(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{}
	c := New("smith@box", fake)

	if err := c.RunWithInput(context.Background(), "cat", strings.NewReader("tskey-abc123"), io.Discard, io.Discard); err != nil {
		t.Fatalf("RunWithInput() error = %v, want nil", err)
	}

	if strings.Contains(strings.Join(fake.args, " "), "tskey-abc123") {
		t.Errorf("argv = %q, want it free of the secret", fake.args)
	}
}

func TestCopyBuildsScpCommand(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{}
	c := New("root@box", fake)

	if err := c.Copy(context.Background(), "/tmp/local.sh", "/tmp/remote.sh"); err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}

	if fake.name != "scp" {
		t.Errorf("launched %q, want scp", fake.name)
	}
	got := strings.Join(fake.args, " ")
	if !strings.Contains(got, "/tmp/local.sh") || !strings.Contains(got, "root@box:/tmp/remote.sh") {
		t.Errorf("scp args = %q, want local path and root@box:/tmp/remote.sh", got)
	}
}

func TestCopyFailureCarriesTheBoxError(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{exitCode: 1, replyStderr: "scp: /tmp/x/script.sh: No space left on device\n"}
	c := New("root@box", fake)

	err := c.Copy(context.Background(), "/tmp/local.sh", "/tmp/x/script.sh")
	if err == nil || !strings.Contains(err.Error(), "No space left on device") {
		t.Errorf("Copy() error = %v, want it to carry scp's stderr", err)
	}
}

func TestRunClassifiesConnectFailure(t *testing.T) {
	t.Parallel()
	fake := &recordingExec{exitCode: 255}
	c := New("root@unreachable", fake)

	err := c.Run(context.Background(), "true", io.Discard, io.Discard)
	if !errors.Is(err, ErrConnect) {
		t.Errorf("Run() error = %v, want wrapping ErrConnect", err)
	}
}

func TestRunNonConnectFailureIsNotConnectError(t *testing.T) {
	t.Parallel()
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

func TestShellArg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain word", in: "public", want: `'public'`},
		{name: "empty string", in: "", want: `''`},
		{name: "spaces", in: "a b", want: `'a b'`},
		{name: "embedded single quote", in: "a'b", want: `'a'\''b'`},
		{name: "leading single quote", in: "'x", want: `''\''x'`},
		{name: "shell metacharacters", in: "$(rm -rf /)", want: `'$(rm -rf /)'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShellArg(tt.in); got != tt.want {
				t.Errorf("ShellArg(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRunReportsAnAuthRefusal(t *testing.T) {
	t.Parallel()
	for _, stderr := range []string{
		"root@box: Permission denied (publickey).\n",
		"Received disconnect from 203.0.113.7 port 22:2: Too many authentication failures\n",
	} {
		t.Run(stderr, func(t *testing.T) {
			t.Parallel()
			c := New("root@box", &recordingExec{exitCode: 255, replyStderr: stderr})

			err := c.Run(context.Background(), "true", io.Discard, io.Discard)
			if !errors.Is(err, ErrAuthRefused) || !errors.Is(err, ErrConnect) {
				t.Fatalf("Run() error = %v, want wrapping ErrAuthRefused and ErrConnect", err)
			}
			for _, want := range []string{"refused the key", "ssh-agent", "Host entry in ~/.ssh/config", "login"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Run() error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

func TestRunReportsAnUnreachableBox(t *testing.T) {
	t.Parallel()
	for _, stderr := range []string{
		"ssh: connect to host box port 22: Connection timed out\n",
		"ssh: connect to host box port 22: Connection refused\n",
		"ssh: connect to host box port 22: No route to host\n",
		"ssh: Could not resolve hostname box: Name or service not known\n",
	} {
		t.Run(stderr, func(t *testing.T) {
			t.Parallel()
			c := New("root@box", &recordingExec{exitCode: 255, replyStderr: stderr})

			err := c.Run(context.Background(), "true", io.Discard, io.Discard)
			if !errors.Is(err, ErrUnreachable) || !errors.Is(err, ErrConnect) {
				t.Fatalf("Run() error = %v, want wrapping ErrUnreachable and ErrConnect", err)
			}
			for _, want := range []string{"did not answer", "address", "port 22", "firewall"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Run() error = %q, want it to mention %q", err, want)
				}
			}
		})
	}
}

func TestRunKeepsSSHsLastLineForAnUnrecognisedConnectFailure(t *testing.T) {
	t.Parallel()
	stderr := "Warning: something earlier\nkex_exchange_identification: read: Connection reset by peer\n"
	c := New("root@box", &recordingExec{exitCode: 255, replyStderr: stderr})

	err := c.Run(context.Background(), "true", io.Discard, io.Discard)
	if !errors.Is(err, ErrConnect) || errors.Is(err, ErrAuthRefused) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("Run() error = %v, want a generic ErrConnect", err)
	}
	if !strings.Contains(err.Error(), "kex_exchange_identification: read: Connection reset by peer") {
		t.Errorf("Run() error = %q, want it to carry ssh's last stderr line", err)
	}
}

func TestRunStillStreamsStderrWhileClassifying(t *testing.T) {
	t.Parallel()
	c := New("root@box", &recordingExec{exitCode: 255, replyStderr: "ssh: connect to host box port 22: Connection refused\n"})

	var stderr bytes.Buffer
	if err := c.Run(context.Background(), "true", io.Discard, &stderr); err == nil {
		t.Fatal("Run() error = nil, want a connect failure")
	}
	if stderr.String() != "ssh: connect to host box port 22: Connection refused\n" {
		t.Errorf("streamed stderr = %q, want ssh's own stderr", stderr.String())
	}
}

func TestRunLeavesARemoteCommandFailureAsItWas(t *testing.T) {
	t.Parallel()
	c := New("root@box", &recordingExec{exitCode: 1, replyStderr: "Permission denied (publickey)\n"})

	err := c.Run(context.Background(), "false", io.Discard, io.Discard)
	if err == nil || err.Error() != "ssh root@box: exit status" {
		t.Errorf("Run() error = %v, want %q", err, "ssh root@box: exit status")
	}
}

func TestCopyReportsAnAuthRefusal(t *testing.T) {
	t.Parallel()
	c := New("root@box", &recordingExec{exitCode: 255, replyStderr: "root@box: Permission denied (publickey).\nscp: Connection closed\n"})

	err := c.Copy(context.Background(), "/tmp/local.sh", "/tmp/x/script.sh")
	if !errors.Is(err, ErrAuthRefused) {
		t.Errorf("Copy() error = %v, want wrapping ErrAuthRefused", err)
	}
}
