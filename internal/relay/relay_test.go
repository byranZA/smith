package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// fakeSSH stands in for the ssh binary, which no test may launch. It records
// the argv it was handed, writes canned output, and fails with a canned error.
type fakeSSH struct {
	calls  [][]string
	stderr string
	err    error
}

func (f *fakeSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.stderr != "" {
		if _, err := io.WriteString(stderr, f.stderr); err != nil {
			return fmt.Errorf("write canned stderr: %w", err)
		}
	}
	return f.err
}

// TestRunRelaysAVerbToTheBox checks the one thing the operator's laptop must
// get right: the box runs the verb, through the absolute path, told which
// smith is calling it.
func TestRunRelaysAVerbToTheBox(t *testing.T) {
	ssh := &fakeSSH{}
	ran := false

	err := Run(context.Background(), ssh, Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "list", "--names"},
	}, func() error { ran = true; return nil }, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Run() err = %v", err)
	}

	if ran {
		t.Error("Run() ran the verb locally, want it relayed to the box")
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh invoked %d times, want 1: %v", len(ssh.calls), ssh.calls)
	}
	line := strings.Join(ssh.calls[0], " ")
	for _, want := range []string{"ssh", "smith@box", "/usr/local/bin/smith", "--relayed-from '0.2.0'", "'session' 'list' '--names'"} {
		if !strings.Contains(line, want) {
			t.Errorf("ssh argv = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, " -t ") {
		t.Errorf("ssh argv = %q, want no TTY requested for a listing", line)
	}
}

// TestRunWithNoTargetRunsTheVerbLocally checks the other half of the one rule:
// the operator who has SSHed in gets the verb here, with no connection opened.
func TestRunWithNoTargetRunsTheVerbLocally(t *testing.T) {
	ssh := &fakeSSH{}
	ran := false

	err := Run(context.Background(), ssh, Verb{
		Version: "0.2.0",
		Args:    []string{"session", "list"},
	}, func() error { ran = true; return nil }, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Run() err = %v", err)
	}

	if !ran {
		t.Error("Run() did not run the verb locally")
	}
	if len(ssh.calls) != 0 {
		t.Errorf("ssh invoked %v, want no connection opened", ssh.calls)
	}
}

// exitStatus is a process error carrying an exit code, the shape os/exec's
// error has and the only part of it the relay reads.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e exitStatus) ExitCode() int { return int(e) }

// TestRunPointsABoxWithNoSmithAtSetup checks the answer every box provisioned
// before smith installed itself gives: 127, which must read as a provisioning
// gap and name the command that closes it.
func TestRunPointsABoxWithNoSmithAtSetup(t *testing.T) {
	ssh := &fakeSSH{
		stderr: "bash: line 1: /usr/local/bin/smith: No such file or directory\n",
		err:    exitStatus(127),
	}

	err := Run(context.Background(), ssh, Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "list"},
	}, func() error { return nil }, io.Discard, io.Discard)

	var absent *NotInstalledError
	if !errors.As(err, &absent) {
		t.Fatalf("Run() err = %v, want a NotInstalledError", err)
	}
	if !strings.Contains(absent.Error(), "smith machine setup") {
		t.Errorf("Run() err = %q, want it to name `smith machine setup`", absent)
	}
	if !strings.Contains(absent.Error(), "smith@box") {
		t.Errorf("Run() err = %q, want it to name the box", absent)
	}
}

// TestRunCarriesTheBoxsRefusalBack checks that a box that ran the verb and
// refused it — the version-skew refusal above all — reaches the operator as
// its own message on stderr and its own exit code, rather than being restated
// or flattened by the relay.
func TestRunCarriesTheBoxsRefusalBack(t *testing.T) {
	ssh := &fakeSSH{
		stderr: "smith: relayed from 0.2.0, but this box runs 0.1.0\n",
		err:    exitStatus(1),
	}
	var errOut strings.Builder

	err := Run(context.Background(), ssh, Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "list"},
	}, func() error { return nil }, io.Discard, &errOut)

	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Run() err = %v, want an ExitError", err)
	}
	if exit.Code != 1 {
		t.Errorf("ExitError.Code = %d, want 1", exit.Code)
	}
	if got := errOut.String(); !strings.Contains(got, "0.2.0") || !strings.Contains(got, "0.1.0") {
		t.Errorf("stderr = %q, want the box's refusal naming both versions", got)
	}
}

// TestRunReportsAnUnreachableBoxAsAConnectFailure checks that ssh failing to
// reach the box at all keeps the meaning internal/connection gave it, rather
// than being read as the box's smith exiting non-zero.
func TestRunReportsAnUnreachableBoxAsAConnectFailure(t *testing.T) {
	ssh := &fakeSSH{err: exitStatus(255)}

	err := Run(context.Background(), ssh, Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "list"},
	}, func() error { return nil }, io.Discard, io.Discard)

	if !errors.Is(err, connection.ErrConnect) {
		t.Fatalf("Run() err = %v, want it to wrap connection.ErrConnect", err)
	}
}

// fakeExecer stands in for replacing smith's own process, which a test may
// never do. It records the argv the kernel would have been handed.
type fakeExecer struct {
	calls [][]string
}

func (f *fakeExecer) Exec(name string, args []string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// TestConnectHandsTheTerminalToTheBox checks the attach path: a TTY is
// requested, smith replaces itself with ssh so nothing sits between the
// operator's terminal and tmux, and the verb still travels as arguments to the
// box's own smith rather than as a tmux command line composed here.
func TestConnectHandsTheTerminalToTheBox(t *testing.T) {
	execer := &fakeExecer{}
	ran := false

	err := Connect(execer, Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "attach", "smith-main", "--interact"},
	}, func() error { ran = true; return nil })
	if err != nil {
		t.Fatalf("Connect() err = %v", err)
	}

	if ran {
		t.Error("Connect() ran the verb locally, want it relayed to the box")
	}
	if len(execer.calls) != 1 {
		t.Fatalf("exec called %d times, want 1: %v", len(execer.calls), execer.calls)
	}
	line := strings.Join(execer.calls[0], " ")
	for _, want := range []string{"ssh", " -t ", "smith@box", "/usr/local/bin/smith", "--relayed-from '0.2.0'", "'session' 'attach' 'smith-main' '--interact'"} {
		if !strings.Contains(line, want) {
			t.Errorf("exec argv = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, "tmux") {
		t.Errorf("exec argv = %q, want no tmux command line composed locally", line)
	}
}

// TestConnectWithNoTargetRunsTheVerbLocally checks that the connecting verbs
// obey the same one rule as the rest: no box named, nothing dialled.
func TestConnectWithNoTargetRunsTheVerbLocally(t *testing.T) {
	execer := &fakeExecer{}
	ran := false

	err := Connect(execer, Verb{Version: "0.2.0", Args: []string{"session", "attach", "smith-main"}}, func() error { ran = true; return nil })
	if err != nil {
		t.Fatalf("Connect() err = %v", err)
	}

	if !ran {
		t.Error("Connect() did not run the verb locally")
	}
	if len(execer.calls) != 0 {
		t.Errorf("exec called %v, want no connection opened", execer.calls)
	}
}
