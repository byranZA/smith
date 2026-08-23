package cli

import "fmt"

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

// Error implements error.
func (e *relayRefusedError) Error() string {
	return fmt.Sprintf("refusing a command relayed from smith %s: this smith is %s, and only an identical version may relay to it", e.relayedFrom, e.local)
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
