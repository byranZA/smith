package cli

import (
	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/relay"
)

// relayRefusedError is on-box smith's answer to a command relayed by a smith of
// a different version: the two binaries may not mean the same thing by the same
// command line, so the command does not run. It names both versions because the
// operator reading it cannot otherwise tell which side is which.
//
// This is the smith binary on the box against the smith binary on the
// operator's machine. It is not the marker's schema_version against the
// constant a build understands — a different axis entirely, and neither
// message borrows the other's words.
type relayRefusedError struct {
	// local is the version of the smith that refused, the one on the box.
	local string
	// relayedFrom is the version the relaying smith declared.
	relayedFrom string
}

// Error implements error. The wording is the relay's own, because the relaying
// smith reads this binary's version back out of it.
func (e *relayRefusedError) Error() string {
	return relay.Refusal(e.local, e.relayedFrom)
}

// relayedBoxFlag is the hidden root flag the relay names the box with, as the
// operator named it. Like --relayed-from it is present only on a relayed
// command line, so its absence is a command typed on the box.
const relayedBoxFlag = "relayed-box"

// invocation is the command an on-box verb answers, for spelling the commands
// it suggests: verb with args as the operator typed them, and the box they
// relayed it to when they did.
func invocation(cmd *cobra.Command, verb string, args []string) hint.Invocation {
	var box string
	if f := cmd.Flag(relayedBoxFlag); f != nil {
		box = f.Value.String()
	}
	return hint.Invocation{Box: box, Verb: verb, Args: args}
}

// acceptRelayedFrom decides whether on-box smith may run a command a relaying
// smith declared relayedFrom for, comparing that declaration against local,
// this binary's own version.
//
// An empty relayedFrom is the operator who SSHed in and ran a verb by hand:
// they are not relaying and have nothing to declare, so nothing is compared.
// Any other value must match local exactly, in either direction, because v0.x
// promises no compatibility and a renamed flag understood loosely produces
// wrong behaviour under a green exit.
func acceptRelayedFrom(local, relayedFrom string) error {
	if relayedFrom == "" || relayedFrom == local {
		return nil
	}
	return &relayRefusedError{local: local, relayedFrom: relayedFrom}
}
