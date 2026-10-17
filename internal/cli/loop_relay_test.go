package cli

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/relay"
)

// laptopLoop is the loop wiring of the operator's own machine, whose inventory registers box dev, relaying over ssh.
func laptopLoop(t *testing.T, ssh connection.Exec, gh *fakeTracker) loopWiring {
	t.Helper()
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	return loopWiring{
		gh:       gh,
		launcher: &idleAgent{},
		lookPath: onPath,
		home:     func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh:      ssh,
		version:  "0.2.0",
	}
}

func TestLoopRunWithABoxRelaysTheRunAndEveryLoopFlag(t *testing.T) {
	ssh := &fakeSSHRelay{}
	gh := readySpec()

	_, stderr, code := runLoopRun(t, laptopLoop(t, ssh, gh), "dev", "42", "--repo", "smith",
		"--agent", "codex", "--model", "o3", "--effort", "high", "--max-iterations", "3", "--max-attempts", "2",
		"--branch", "feat/x", "--base", "dev", "--no-push")

	if code != 0 {
		t.Fatalf("exit %d, stderr %q; want 0", code, stderr)
	}
	line := ssh.line(t)
	want := "/usr/local/bin/smith --relayed-from '0.2.0' --relayed-box 'dev' 'loop' 'run' '42' '--agent=codex' '--base=dev' '--branch=feat/x' " +
		"'--effort=high' '--max-attempts=2' '--max-iterations=3' '--model=o3' '--no-push=true' '--repo=smith'"
	if !strings.HasPrefix(line, "ssh") || !strings.Contains(line, "smith@100.92.14.7") || !strings.Contains(line, want) {
		t.Errorf("ssh argv = %q, want it to reach smith@100.92.14.7 running %q", line, want)
	}
	if len(gh.ran) != 0 {
		t.Errorf("gh = %v, want a relayed run to read nothing here", gh.ran)
	}
}

func TestLoopRunWithABoxStreamsTheBoxsOutputAndExitBack(t *testing.T) {
	ssh := &fakeSSHRelay{stdout: "agent:  claude\n", stderr: "smith: agent claude is not installed\n", err: relayExit(1)}

	stdout, stderr, code := runLoopRun(t, laptopLoop(t, ssh, readySpec()), "dev", "42", "--repo", "smith")

	if code != 1 || stdout != "agent:  claude\n" || stderr != "smith: agent claude is not installed\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want the box's exit 1 and its own output", code, stdout, stderr)
	}
}

func TestLoopRunWithABoxRefusesInteractiveBeforeRelaying(t *testing.T) {
	ssh := &fakeSSHRelay{}

	_, stderr, code := runLoopRun(t, laptopLoop(t, ssh, readySpec()), "dev", "42", "--repo", "smith", "--interactive")

	want := "--interactive cannot run on a box: start a human session with `smith session start dev --repo smith --branch <branch>` and run `smith loop run 42 --interactive` in it"
	if code != 1 || !strings.Contains(stderr, want) || len(ssh.calls) != 0 {
		t.Errorf("exit %d, stderr %q, ssh %v; want 1, %q and nothing relayed", code, stderr, ssh.calls, want)
	}
}

func TestLoopRunWithABoxNeedsARepo(t *testing.T) {
	ssh := &fakeSSHRelay{}

	_, stderr, code := runLoopRun(t, laptopLoop(t, ssh, readySpec()), "dev", "42")

	want := "loop run on box dev needs --repo naming the repo on the box to run in"
	if code != 1 || !strings.Contains(stderr, want) || len(ssh.calls) != 0 {
		t.Errorf("exit %d, stderr %q, ssh %v; want 1, %q and nothing relayed", code, stderr, ssh.calls, want)
	}
}

func TestLoopRunWithASkewedBoxConvergesItThenRunsAgain(t *testing.T) {
	ssh := &skewSSH{refuse: true, stdout: "session smith-smith-42-the-loop is live\n"}
	conv := &convergeRecorder{ssh: ssh}
	w := laptopLoop(t, ssh, readySpec())
	w.skew = skew{term: &scriptedTerminal{attended: true, answer: "y"}, converge: conv.converge}

	stdout, stderr, code := runLoopRun(t, w, "dev", "42", "--repo", "smith")

	if code != 0 || len(conv.calls) != 1 || conv.calls[0] != "dev 0.2.0" || len(ssh.calls) != 2 || !strings.HasSuffix(stdout, "session smith-smith-42-the-loop is live\n") {
		t.Errorf("exit %d, converged %v, ssh %d, stdout %q, stderr %q; want box dev converged to 0.2.0 and the run relayed again", code, conv.calls, len(ssh.calls), stdout, stderr)
	}
}

func TestLoopRunWithASkewedBoxUnattendedIsRefused(t *testing.T) {
	ssh := &skewSSH{refuse: true}

	_, stderr, code := runLoopRun(t, laptopLoop(t, ssh, readySpec()), "dev", "42", "--repo", "smith")

	if code != relay.RefusalExitCode || !strings.Contains(stderr, "smith machine upgrade dev") {
		t.Errorf("exit %d, stderr %q; want %d pointing at `smith machine upgrade dev`", code, stderr, relay.RefusalExitCode)
	}
}
