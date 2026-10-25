package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/hint"
)

// attended reports whether a human is at term. No terminal at all is a run
// nobody is attending.
func attended(term terminal) bool {
	return term != nil && term.Attended()
}

// relayedOr is inv as the operator would type its suggestions on this machine:
// naming box when they named one here, and otherwise the box the command was
// relayed to, if any.
func relayedOr(inv hint.Invocation, box string) hint.Invocation {
	if box != "" {
		inv.Box = box
	}
	return inv
}

// refuseUnattended ends a verb that hands the operator's terminal over when
// nobody is at one, before it reaches anything, and names instead, the command
// a caller with no terminal runs in its place.
func refuseUnattended(cmd *cobra.Command, verb, instead string) error {
	return reportInvalid(cmd, fmt.Errorf("no terminal is attached, and %s hands one over: run `%s` instead", verb, instead))
}

// listInstead is what an attach with nobody at the terminal points at: the
// listing of the sessions there are.
func listInstead(inv hint.Invocation, _ []string) string {
	return inv.Command("session list")
}

// detachInstead is what a connecting start with nobody at the terminal points
// at: the same start, standing the session up without connecting to it.
func detachInstead(inv hint.Invocation, args []string) string {
	return inv.Command("session start", append(args, "--detach")...)
}

// attachedInstead is what an interactive loop run with nobody at the terminal
// points at: the same run as the operator typed it, without --interactive.
func attachedInstead(cmd *cobra.Command, box string, args []string) string {
	var kept []string
	for _, arg := range typed(cmd, args) {
		if arg != "--interactive=true" {
			kept = append(kept, arg)
		}
	}
	return relayedOr(invocation(cmd, "loop run", kept), box).Command("loop run", kept...)
}

// yesFlag is the root flag that answers yes, up front, to every [y/N] question
// smith would ask.
const yesFlag = "yes"

// addYesFlag gives root the --yes every verb inherits. It answers questions
// only: a secret is never answered by it, and a verb that hands the terminal
// over still refuses with nobody at one.
func addYesFlag(root *cobra.Command) {
	root.PersistentFlags().Bool(yesFlag, false, "answer yes to every [y/N] question without asking, attended or not; it never answers a secret prompt")
}

// assumeYes reports whether the operator answered yes up front with --yes. A
// command built without smith's root has no such flag, and is asked.
func assumeYes(cmd *cobra.Command) bool {
	f := cmd.Flag(yesFlag)
	return f != nil && f.Value.String() == "true"
}

// The sentences a verb's help says about a run with nobody at the terminal, so
// every verb says it in the same words. handsOverHelp is a verb that hands the
// terminal over; skewHelp is a verb that relays to a box, which may refuse it
// for running another smith.
const (
	handsOverHelp = "With nobody at the terminal it refuses at once, naming a command that needs none."
	skewHelp      = "A box running another smith version is converged only once you accept, or with --yes; with nobody at the terminal, its upgrade command is printed instead."
)

// help renders a verb's long help: what it does, then what it says about a run
// with nobody at the terminal.
func help(does string, unattended ...string) string {
	return does + "\n\n" + strings.Join(unattended, " ")
}
