package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// relayExit is the process error a relayed smith that ran and exited non-zero
// reports, carrying the status the relay reads.
type relayExit int

func (e relayExit) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e relayExit) ExitCode() int { return int(e) }

// fakeRunner records the argv of every command launched through it and reports
// success. A relayed verb must reach neither git nor tmux on the operator's
// own machine, and this is what proves it did not.
type fakeRunner struct {
	calls [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil
}

// fakeSSHRelay stands in for the local ssh binary on the relay path: it
// records the argv it was handed, writes what the box would have written, and
// fails with the status the box would have exited with.
type fakeSSHRelay struct {
	calls  [][]string
	stdout string
	stderr string
	err    error
}

func (f *fakeSSHRelay) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if _, err := io.WriteString(stdout, f.stdout); err != nil {
		return fmt.Errorf("write canned stdout: %w", err)
	}
	if _, err := io.WriteString(stderr, f.stderr); err != nil {
		return fmt.Errorf("write canned stderr: %w", err)
	}
	return f.err
}

// line returns the argv of the one ssh launch, joined for readability.
func (f *fakeSSHRelay) line(t *testing.T) string {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("ssh launched %d times, want 1: %v", len(f.calls), f.calls)
	}
	return strings.Join(f.calls[0], " ")
}

// runSession drives the assembled session command the way an operator does,
// with every boundary faked, and returns what landed on each stream plus the
// exit code. dir is the operator's config home, which is where the box name
// they typed is resolved.
func runSessionOn(t *testing.T, w sessionWiring, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newSessionCmd(w)
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// laptop assembles the session command as it runs on the operator's own
// machine: a config home holding the inventory, and no box of its own.
func laptop(t *testing.T, dir string, ssh *fakeSSHRelay, execer *fakeExec, git, tmux *fakeRunner) sessionWiring {
	t.Helper()
	return sessionWiring{
		box:     resolvedBox(t.TempDir()),
		home:    func() (config.Home, error) { return config.NewHome(dir), nil },
		root:    t.TempDir(),
		git:     git,
		tmux:    tmux,
		connect: execer,
		ssh:     ssh,
		version: "0.2.0",
	}
}

// TestSessionVerbWithABoxRunsOnTheBox checks the one rule from the operator's
// side: naming a box sends the verb there, over ssh, and nothing about git or
// tmux is decided on the laptop.
func TestSessionVerbWithABoxRunsOnTheBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &fakeSSHRelay{stdout: "smith-main  live  -  0\n"}
	git, tmux := &fakeRunner{}, &fakeRunner{}

	stdout, stderr, code := runSessionOn(t, laptop(t, dir, ssh, &fakeExec{}, git, tmux), "list", "dev", "--names")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	line := ssh.line(t)
	for _, want := range []string{"smith@100.92.14.7", "/usr/local/bin/smith", "--relayed-from '0.2.0'", "'session' 'list'", "'--names=true'"} {
		if !strings.Contains(line, want) {
			t.Errorf("ssh argv = %q, want it to contain %q", line, want)
		}
	}
	if len(git.calls) != 0 || len(tmux.calls) != 0 {
		t.Errorf("git = %v, tmux = %v, want a relayed verb to compose neither", git.calls, tmux.calls)
	}
	if !strings.Contains(stdout, "smith-main") {
		t.Errorf("stdout = %q, want the box's own output", stdout)
	}
}

// TestSessionVerbWithNoBoxRunsHere checks the other half: the operator who has
// connected to the box types the same verb with no target and gets it here,
// with no connection opened.
func TestSessionVerbWithNoBoxRunsHere(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	w := laptop(t, t.TempDir(), &fakeSSHRelay{}, &fakeExec{}, &fakeRunner{}, &fakeRunner{})
	w.box = resolvedBox(workspace, "smith")
	w.git = connection.System()

	stdout, stderr, code := runSessionOn(t, w, "list")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if ssh := w.ssh.(*fakeSSHRelay); len(ssh.calls) != 0 {
		t.Errorf("ssh launched %v, want no connection opened", ssh.calls)
	}
	if !strings.Contains(stdout, "no sessions") {
		t.Errorf("stdout = %q, want the local listing", stdout)
	}
}

// TestSessionAttachRelaysWithATerminal checks that attach travels as the same
// session verb the operator typed, with a TTY requested, and that no tmux
// session name is composed on the laptop.
func TestSessionAttachRelaysWithATerminal(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	execer := &fakeExec{}

	_, stderr, code := runSessionOn(t, laptop(t, dir, &fakeSSHRelay{}, execer, &fakeRunner{}, &fakeRunner{}), "attach", "dev", "smith-main", "--interact")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(execer.calls) != 1 {
		t.Fatalf("exec called %d times, want 1: %v", len(execer.calls), execer.calls)
	}
	line := strings.Join(execer.calls[0], " ")
	for _, want := range []string{"ssh", " -t ", "smith@100.92.14.7", "'session' 'attach' 'smith-main'", "'--interact=true'"} {
		if !strings.Contains(line, want) {
			t.Errorf("exec argv = %q, want it to contain %q", line, want)
		}
	}
	if strings.Contains(line, "tmux") {
		t.Errorf("exec argv = %q, want no tmux command line composed here", line)
	}
}

// TestSessionVerbOnABoxWithNoSmithPointsAtSetup checks the answer every box
// provisioned before smith installed itself gives, and that it reads as a
// provisioning gap rather than as a broken verb — named by the box the
// operator typed, since the setup command they are pointed at takes that
// spelling and not the address it resolved to.
func TestSessionVerbOnABoxWithNoSmithPointsAtSetup(t *testing.T) {
	for _, tc := range []struct {
		name string
		box  string
	}{
		{name: "a registered box name", box: "dev"},
		{name: "a literal target", box: "smith@100.92.14.7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
			ssh := &fakeSSHRelay{
				stderr: "bash: line 1: /usr/local/bin/smith: No such file or directory\n",
				err:    relayExit(127),
			}

			_, stderr, code := runSessionOn(t, laptop(t, dir, ssh, &fakeExec{}, &fakeRunner{}, &fakeRunner{}), "list", tc.box)

			if code == 0 {
				t.Fatal("exit code = 0, want the verb to fail")
			}
			if want := "box " + tc.box + " has no smith installed"; !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, want)
			}
			if want := "`smith machine setup " + tc.box + "`"; !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want it to suggest %q", stderr, want)
			}
		})
	}
}

// TestSessionVerbCarriesTheBoxsRefusalBack checks that a box that refuses the
// relay — a smith of a different version above all — reaches the operator as
// its own message and its own exit code, with local smith adding nothing.
func TestSessionVerbCarriesTheBoxsRefusalBack(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &fakeSSHRelay{
		stderr: "smith: relayed from 0.2.0, but this box runs 0.1.0: converge it with `smith machine upgrade dev`\n",
		err:    relayExit(2),
	}
	git := &fakeRunner{}

	_, stderr, code := runSessionOn(t, laptop(t, dir, ssh, &fakeExec{}, git, &fakeRunner{}), "list", "dev")

	if code != 2 {
		t.Errorf("exit code = %d, want the box's own 2", code)
	}
	for _, want := range []string{"0.2.0", "0.1.0"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to name version %s", stderr, want)
		}
	}
	if len(git.calls) != 0 {
		t.Errorf("git = %v, want a refused relay to have run nothing", git.calls)
	}
}

// TestSessionRemoveSeparatesTheBoxFromTheNames checks the one verb whose
// leading argument is ambiguous: a registered box relays the removal, and a
// batch of bare names is a batch of names.
func TestSessionRemoveSeparatesTheBoxFromTheNames(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &fakeSSHRelay{}

	_, stderr, code := runSessionOn(t, laptop(t, dir, ssh, &fakeExec{}, &fakeRunner{}, &fakeRunner{}), "rm", "dev", "smith-main", "smith-spec-42")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got, want := ssh.line(t), "'session' 'rm' 'smith-main' 'smith-spec-42'"; !strings.Contains(got, want) {
		t.Errorf("ssh argv = %q, want it to contain %q", got, want)
	}
}

// TestSessionRemoveTakesBareNamesLocally checks the same verb on the box: no
// argument names a box, so every argument is a session and nothing is dialled.
func TestSessionRemoveTakesBareNamesLocally(t *testing.T) {
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith")
	ssh := &fakeSSHRelay{}
	w := laptop(t, t.TempDir(), ssh, &fakeExec{}, &fakeRunner{}, &fakeRunner{})
	w.box, w.git = resolvedBox(workspace, "smith"), connection.System()

	_, stderr, code := runSessionOn(t, w, "rm", "smith-main", "smith-spec-42")

	if code == 0 {
		t.Fatal("exit code = 0, want the local removal to refuse two names no session holds")
	}
	if !strings.Contains(stderr, "smith-spec-42") {
		t.Errorf("stderr = %q, want the local refusal to name the session asked for", stderr)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("ssh launched %v, want no connection opened", ssh.calls)
	}
}

// neverSSH stands in for the local ssh binary in a test where the verb runs on
// this machine: launching it at all is the failure.
type neverSSH struct {
	t *testing.T
}

func (s neverSSH) Run(_ context.Context, name string, args []string, _ io.Reader, _, _ io.Writer) error {
	s.t.Helper()
	s.t.Errorf("ssh launched as %v, want a verb with no box named to open no connection", append([]string{name}, args...))
	return nil
}

// onBox assembles the session verbs the way the operator who has connected to
// the box gets them: a box of their own to act on, an empty config home, and
// an ssh that may not be launched.
func onBox(t *testing.T, resolve boxResolver, root string, git, tmux session.Runner, connect session.Execer) sessionWiring {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	return sessionWiring{
		box:     resolve,
		home:    func() (config.Home, error) { return config.NewHome(home), nil },
		root:    root,
		git:     git,
		tmux:    tmux,
		connect: connect,
		ssh:     neverSSH{t: t},
		version: "0.2.0",
	}
}
