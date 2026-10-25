package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/agent"
)

// forbidden is every boundary a refused verb must not reach, failing the test when one is.
type forbidden struct {
	t *testing.T
}

func (f forbidden) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.t.Helper()
	f.t.Errorf("ran %v, want nothing launched by a verb with nobody at the terminal", append([]string{name}, args...))
	return nil
}

func (f forbidden) Exec(name string, args []string) error {
	f.t.Helper()
	f.t.Errorf("exec'd %v, want no terminal handed over with nobody at it", append([]string{name}, args...))
	return nil
}

// unattendedLaptop is the operator's machine with nobody at the terminal and every boundary forbidden.
func unattendedLaptop(t *testing.T) sessionWiring {
	t.Helper()
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	w := laptop(t, dir, &fakeSSHRelay{}, &fakeExec{}, &fakeRunner{}, &fakeRunner{})
	w.ssh, w.git, w.tmux, w.connect, w.mise = forbidden{t}, forbidden{t}, forbidden{t}, forbidden{t}, forbidden{t}
	w.term = &scriptedTerminal{attended: false}
	return w
}

func TestSessionAttachUnattendedRefusesBeforeConnecting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"relayed", []string{"attach", "dev", "smith-main"}, "smith session list dev"},
		{"on the box", []string{"attach", "smith-main"}, "smith session list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runSessionOn(t, unattendedLaptop(t), tt.args...)

			if code != 1 {
				t.Errorf("session %v exit code = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr, "no terminal is attached") || !strings.Contains(stderr, "`"+tt.want+"`") {
				t.Errorf("session %v stderr = %q, want it to say no terminal is attached and name `%s`", tt.args, stderr, tt.want)
			}
		})
	}
}

func TestSessionStartUnattendedRefusesNamingTheDetachedStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"relayed", []string{"start", "dev", "--repo", "smith", "--branch", "spec-42"}, "smith session start dev --branch=spec-42 --repo=smith --detach"},
		{"on the box", []string{"start", "--repo", "smith", "--branch", "spec-42"}, "smith session start --branch=spec-42 --repo=smith --detach"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, code := runSessionOn(t, unattendedLaptop(t), tt.args...)

			if code != 1 {
				t.Errorf("session %v exit code = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr, "no terminal is attached") || !strings.Contains(stderr, "`"+tt.want+"`") {
				t.Errorf("session %v stderr = %q, want it to say no terminal is attached and name `%s`", tt.args, stderr, tt.want)
			}
		})
	}
}

func TestSessionStartDetachedUnattendedStandsTheSessionUp(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, forbidden{t})
	w.term = &scriptedTerminal{attended: false}

	stdout, stderr, code := runSessionOn(t, w, "start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("session start --detach exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "session smith-spec-42 is live on branch spec-42 of smith\n"; stdout != want {
		t.Errorf("session start --detach stdout = %q, want %q", stdout, want)
	}
}

func (f forbidden) Launch(_ context.Context, cmd agent.Command) error {
	f.t.Helper()
	f.t.Errorf("launched agent %v, want no agent attached to a terminal nobody is at", cmd.Args)
	return nil
}

func TestLoopRunInteractiveUnattendedRefusesBeforeAnything(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"here", []string{"42", "--interactive"}, "smith loop run 42"},
		{"relayed", []string{"dev", "42", "--repo", "smith", "--interactive"}, "smith loop run dev 42 --repo=smith"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := laptopLoop(t, forbidden{t}, nil)
			w.gh, w.launcher, w.git = forbidden{t}, forbidden{t}, forbidden{t}
			w.workdir = func() (string, error) { return t.TempDir(), nil }
			w.term = &scriptedTerminal{attended: false}

			_, stderr, code := runLoopRun(t, w, tt.args[0], tt.args[1:]...)

			if code != 1 {
				t.Errorf("loop run %v exit code = %d, want 1", tt.args, code)
			}
			if !strings.Contains(stderr, "no terminal is attached") || !strings.Contains(stderr, "`"+tt.want+"`") {
				t.Errorf("loop run %v stderr = %q, want it to say no terminal is attached and name `%s`", tt.args, stderr, tt.want)
			}
		})
	}
}

// underRootWith runs cmd with args beneath a root carrying smith's inherited --yes.
func underRootWith(t *testing.T, cmd *cobra.Command, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	root := &cobra.Command{Use: "smith", SilenceUsage: true, SilenceErrors: true}
	addYesFlag(root)
	root.AddCommand(cmd)
	root.SetArgs(args)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	code = codeFromError(root.Execute())
	return out.String(), errBuf.String(), code
}

func TestSkewWithYesConvergesAndRerunsWithoutAsking(t *testing.T) {
	t.Parallel()
	for _, attended := range []bool{true, false} {
		t.Run(fmt.Sprintf("attended=%v", attended), func(t *testing.T) {
			t.Parallel()
			ssh := &skewSSH{refuse: true, stdout: "smith-main  live  -  0\n"}
			conv := &convergeRecorder{ssh: ssh}
			term := &scriptedTerminal{attended: attended, answer: "n"}

			stdout, stderr, code := underRootWith(t, newSessionCmd(skewLaptop(t, ssh, term, conv)), "--yes", "session", "list", "dev")

			if code != 0 {
				t.Fatalf("session list dev --yes exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			if term.asked {
				t.Errorf("session list dev --yes asked %q, want nothing asked", term.question)
			}
			if len(conv.calls) != 1 || conv.calls[0] != "dev 0.2.0" {
				t.Errorf("session list dev --yes converged %v, want [dev 0.2.0]", conv.calls)
			}
			if len(ssh.calls) != 2 || !strings.Contains(stdout, "smith-main") {
				t.Errorf("session list dev --yes ran ssh %d times with stdout %q, want the refused relay, then the re-run's output", len(ssh.calls), stdout)
			}
		})
	}
}

func TestRelayedVerbWithYesLeavesItOffTheBoxCommandLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &fakeSSHRelay{stdout: "smith-main\n"}

	_, stderr, code := underRootWith(t, newSessionCmd(laptop(t, dir, ssh, &fakeExec{}, &fakeRunner{}, &fakeRunner{})), "--yes", "session", "list", "dev", "--names")

	if code != 0 {
		t.Fatalf("session list dev --yes exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	want := "/usr/local/bin/smith --relayed-from '0.2.0' --relayed-box 'dev' 'session' 'list' '--names=true'"
	if line := ssh.line(t); !strings.HasSuffix(line, want) {
		t.Errorf("session list dev --yes ssh argv = %q, want it to end with %q", line, want)
	}
}

func TestSmithAcceptsYesOnARelayedCommandLine(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	root.SetArgs([]string{"--relayed-from", resolveVersion(), "--relayed-box", "dev", "--yes", "version"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	if code := codeFromError(root.Execute()); code != 0 {
		t.Errorf("smith --yes version relayed exit code = %d, want 0 (stderr: %s)", code, errBuf.String())
	}
}

func TestHelpSaysWhatEachAffectedVerbDoesWithNobodyAtTheTerminal(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"session", "attach"},
		{"session", "start"},
		{"session", "list"},
		{"session", "stop"},
		{"session", "rm"},
		{"workspace", "converge"},
		{"loop", "run"},
		{"update"},
	} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			t.Parallel()
			found, _, err := newRootCmd().Find(path)
			if err != nil {
				t.Fatalf("Find(%v) err = %v", path, err)
			}
			if !strings.Contains(strings.ToLower(found.Long), "with nobody at the terminal") {
				t.Errorf("smith %s --help = %q, want it to say what it does with nobody at the terminal", strings.Join(path, " "), found.Long)
			}
		})
	}
}
