package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/relay"
)

// scriptedTerminal stands in for the operator's terminal: it answers with a
// canned line and records whether it was asked at all, which is what proves an
// unattended run was never made to wait for input.
type scriptedTerminal struct {
	attended bool
	answer   string
	err      error
	asked    bool
	question string
	order    *[]string
}

func (s *scriptedTerminal) Attended() bool {
	if s.order != nil {
		*s.order = append(*s.order, "terminal")
	}
	return s.attended
}

func (s *scriptedTerminal) ReadLine(prompt string) (string, error) {
	s.asked = true
	s.question = prompt
	return s.answer, s.err
}

// skewSSH is the box that refuses a relayed command until its smith is
// converged, and runs the verb once it is.
type skewSSH struct {
	calls  [][]string
	refuse bool
	stdout string
	order  *[]string
}

func (f *skewSSH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.order != nil {
		*f.order = append(*f.order, "ssh")
	}
	if f.refuse {
		if _, err := io.WriteString(stderr, relay.Refusal("0.3.0", "0.2.0")+"\n"); err != nil {
			return fmt.Errorf("write canned refusal: %w", err)
		}
		return relayExit(relay.RefusalExitCode)
	}
	if _, err := io.WriteString(stdout, f.stdout); err != nil {
		return fmt.Errorf("write canned stdout: %w", err)
	}
	return nil
}

// convergeRecorder is the install stage the accepted prompt reaches, recording
// what it was asked to converge and clearing the box's refusal.
type convergeRecorder struct {
	calls []string
	ssh   *skewSSH
	err   error
}

func (c *convergeRecorder) converge(_ context.Context, box, version string) (string, error) {
	c.calls = append(c.calls, box+" "+version)
	if c.err != nil {
		return "", c.err
	}
	if c.ssh != nil {
		c.ssh.refuse = false
	}
	return fmt.Sprintf("box %s: smith 0.3.0 → %s (matching local smith)\n", box, version), nil
}

// skewLaptop assembles the session command as it runs on the operator's own
// machine against a box that refuses the relay, wired to the terminal and the
// install stage the refusal is reacted to through.
func skewLaptop(t *testing.T, ssh *skewSSH, term *scriptedTerminal, conv *convergeRecorder) sessionWiring {
	t.Helper()
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	w := laptop(t, dir, &fakeSSHRelay{}, &fakeExec{}, &fakeRunner{}, &fakeRunner{})
	w.ssh = ssh
	w.skew = skew{term: term, converge: conv.converge}
	return w
}

// TestSkewPromptNamesBothVersionsAndTheDirection checks the question an
// attended terminal is asked: which side runs what, and which way the
// convergence goes.
func TestSkewPromptNamesBothVersionsAndTheDirection(t *testing.T) {
	ssh := &skewSSH{refuse: true}
	term := &scriptedTerminal{attended: true, answer: "n"}

	_, _, _ = runSessionOn(t, skewLaptop(t, ssh, term, &convergeRecorder{}), "list", "dev")

	if !term.asked {
		t.Fatal("an attended terminal was not asked anything")
	}
	for _, want := range []string{"dev", "0.3.0", "0.2.0", "converge", "[y/N]"} {
		if !strings.Contains(term.question, want) {
			t.Errorf("question = %q, want it to contain %q", term.question, want)
		}
	}
}

// TestSkewAcceptedConvergesTheBoxThenRunsTheCommand checks the half that makes
// the prompt worth asking: yes converges the box and the original command then
// runs, rather than the operator being handed a suggestion.
func TestSkewAcceptedConvergesTheBoxThenRunsTheCommand(t *testing.T) {
	ssh := &skewSSH{refuse: true, stdout: "smith-main  live  -  0\n"}
	conv := &convergeRecorder{ssh: ssh}
	term := &scriptedTerminal{attended: true, answer: "y"}

	stdout, stderr, code := runSessionOn(t, skewLaptop(t, ssh, term, conv), "list", "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(conv.calls) != 1 || conv.calls[0] != "dev 0.2.0" {
		t.Errorf("converged %v, want one convergence of box dev to 0.2.0", conv.calls)
	}
	if len(ssh.calls) != 2 {
		t.Errorf("ssh launched %d times, want the refused relay and then the re-run", len(ssh.calls))
	}
	if !strings.Contains(stdout, "smith-main") {
		t.Errorf("stdout = %q, want the box's own output once the command ran", stdout)
	}
}

// TestSkewDeclinedPrintsTheUpgradeCommand checks the other half: no converges
// nothing, names the exact command for that box, and fails.
func TestSkewDeclinedPrintsTheUpgradeCommand(t *testing.T) {
	for _, answer := range []string{"n", "", "  "} {
		ssh := &skewSSH{refuse: true}
		conv := &convergeRecorder{ssh: ssh}
		term := &scriptedTerminal{attended: true, answer: answer}

		_, stderr, code := runSessionOn(t, skewLaptop(t, ssh, term, conv), "list", "dev")

		if code == 0 {
			t.Errorf("answer %q: exit code = 0, want a refused relay to fail", answer)
		}
		if !strings.Contains(stderr, "smith machine upgrade dev") {
			t.Errorf("answer %q: stderr = %q, want the upgrade command for the box", answer, stderr)
		}
		if len(conv.calls) != 0 {
			t.Errorf("answer %q: converged %v, want nothing", answer, conv.calls)
		}
		if len(ssh.calls) != 1 {
			t.Errorf("answer %q: ssh launched %d times, want only the refused relay", answer, len(ssh.calls))
		}
	}
}

// TestSkewUnattendedPrintsWithoutWaiting checks the path an AFK loop inherits:
// no terminal is asked anything, the command to type is printed, and the run
// fails rather than blocking.
func TestSkewUnattendedPrintsWithoutWaiting(t *testing.T) {
	ssh := &skewSSH{refuse: true}
	conv := &convergeRecorder{ssh: ssh}
	term := &scriptedTerminal{attended: false, answer: "y"}

	_, stderr, code := runSessionOn(t, skewLaptop(t, ssh, term, conv), "list", "dev")

	if code == 0 {
		t.Fatal("exit code = 0, want an unattended refused relay to fail")
	}
	if term.asked {
		t.Error("an unattended run waited for input")
	}
	if !strings.Contains(stderr, "smith machine upgrade dev") {
		t.Errorf("stderr = %q, want the upgrade command for the box", stderr)
	}
	if len(conv.calls) != 0 {
		t.Errorf("converged %v, want nothing", conv.calls)
	}
}

// TestSkewChecksTheTerminalBeforeConnecting checks whose terminal is being
// judged: local smith's own, settled before the verb travels, so nothing about
// the box's end can decide whether a human is there.
func TestSkewChecksTheTerminalBeforeConnecting(t *testing.T) {
	var order []string
	ssh := &skewSSH{refuse: true, order: &order}
	term := &scriptedTerminal{attended: false, order: &order}

	_, _, _ = runSessionOn(t, skewLaptop(t, ssh, term, &convergeRecorder{}), "list", "dev")

	if len(order) < 2 || order[0] != "terminal" {
		t.Errorf("order = %v, want the terminal consulted before ssh ran", order)
	}
}

// TestSkewPromptsInBothDirections checks that a box behind local smith is
// offered the same fix as one ahead of it: suppressing the prompt would leave
// the operator with a refusal and nothing to do about it.
func TestSkewPromptsInBothDirections(t *testing.T) {
	ssh := &skewSSH{refuse: true, stdout: "smith-main  live  -  0\n"}
	conv := &convergeRecorder{ssh: ssh}
	term := &scriptedTerminal{attended: true, answer: "y"}
	w := skewLaptop(t, ssh, term, conv)
	w.version = "0.4.0"

	_, stderr, code := runSessionOn(t, w, "list", "dev")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(term.question, "0.4.0") {
		t.Errorf("question = %q, want the operator's newer version named", term.question)
	}
	if len(conv.calls) != 1 || conv.calls[0] != "dev 0.4.0" {
		t.Errorf("converged %v, want box dev moved back to 0.4.0", conv.calls)
	}
}
