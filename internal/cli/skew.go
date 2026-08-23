package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/relay"
)

// terminal is the operator's own terminal, as the version-skew prompt needs
// it: whether a human is attending it, and one line of what they typed. It is
// satisfied by secret.StdTerminal, so the prompt reuses the one terminal
// abstraction smith has rather than asking the operating system a second time
// whether a stream is a terminal — which is also what lets a test answer the
// question with a scripted line.
type terminal interface {
	// Attended reports whether a human is there to be asked.
	Attended() bool
	// ReadLine writes prompt and reads one echoed line of the answer.
	ReadLine(prompt string) (string, error)
}

// skew is what local smith does about a box that refused a relayed command
// because the two smiths are different versions: ask the human at the terminal
// whether to converge the box and then carry on with what they typed, and tell
// an unattended run what to type instead.
//
// The reaction lives here and not in internal/relay because it is a decision
// about the operator, not about the wire: the relay classifies the refusal and
// returns it typed, and this is the only code that turns that outcome into a
// question, a printed command, or a convergence.
type skew struct {
	// term is the terminal the question is asked on. A zero skew has none,
	// which is an unattended run: it prints rather than prompts.
	term terminal
	// converge is the convergence an accepted question reaches: the same
	// operation `smith machine upgrade` runs, over the same ssh boundary the
	// refused relay travelled on.
	converge namedBoxConverger
}

// react runs a relayed verb and reacts to the one failure the operator can be
// offered a fix for. A verb that succeeded, or failed any other way, travels
// out untouched; a version-skew refusal is put to the terminal, and answering
// yes converges the box and runs the verb again, because a prompt that only
// prints a suggestion is a slower way to print a suggestion.
//
// Whether a human is there is settled before the verb travels, on this
// machine's own streams. The check is local smith's, not the box's: it is
// unrelated to the BatchMode=yes that forbids ssh from prompting for
// credentials, and unrelated to the terminal an attaching verb asks the box
// for.
func (s skew) react(cmd *cobra.Command, box string, run func() error) error {
	if box == "" {
		return run()
	}
	attended := s.term != nil && s.term.Attended()

	err := run()
	var mismatch *relay.MismatchError
	if !errors.As(err, &mismatch) {
		return err
	}
	if !attended {
		return refuseSkew(cmd, box)
	}
	accepted, askErr := s.ask(box, mismatch)
	if askErr != nil {
		return askErr
	}
	if !accepted {
		return refuseSkew(cmd, box)
	}
	converged, err := s.converge(cmd.Context(), box, mismatch.Local)
	if err != nil {
		return reportInvalid(cmd, err)
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), converged.Report()); err != nil {
		return fmt.Errorf("write upgrade report: %w", err)
	}
	return run()
}

// ask puts the convergence to the operator and reports whether they accepted.
func (s skew) ask(box string, mismatch *relay.MismatchError) (bool, error) {
	answer, err := s.term.ReadLine(skewQuestion(box, mismatch))
	if err != nil {
		return false, fmt.Errorf("ask whether to converge box %s: %w", box, err)
	}
	return accepted(answer), nil
}

// accepted reads the answer to a [y/N] question. Only an explicit yes is one,
// so a bare Enter declines and so does a typo: the question mutates a box, and
// the fix for a wrong no is to run the command again.
func accepted(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// skewQuestion renders the question, naming which side runs what and which way
// the convergence goes. It fires in both directions: a box behind local smith
// is as refused as one ahead of it, and suppressing the question there would
// leave the operator with a refusal and no offered fix.
//
// A box whose refusal named no version it knows how to read is still worth
// asking about — the exit code already said what happened, and the convergence
// on offer is the same one.
func skewQuestion(box string, mismatch *relay.MismatchError) string {
	runs := "a different version of smith"
	if mismatch.Box != "" {
		runs = mismatch.Box
	}
	return fmt.Sprintf("box %s runs %s, you run %s — converge box %s to %s? [y/N] ",
		box, runs, mismatch.Local, box, mismatch.Local)
}

// refuseSkew ends a run whose convergence was declined or never offered: it
// names the exact command that converges the box, converges nothing, and exits
// with the code the box refused on. That is the path an AFK loop inherits, and
// it never blocks.
func refuseSkew(cmd *cobra.Command, box string) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "converge the box first:\n  smith machine upgrade %s\n", box); err != nil {
		return fmt.Errorf("write the upgrade command: %w", err)
	}
	return &exitError{code: relay.RefusalExitCode}
}
