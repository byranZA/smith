// Package cli is the cobra command surface for smith. It stays thin: each RunE
// parses flags, calls the domain packages, and formats the result. Process
// exit codes are owned here so main can stay a one-liner.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/provider"
	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/staging"
)

// buildVersion is the smith version a release pipeline injects via
// -ldflags "-X …/internal/cli.buildVersion=<version>". It is a var solely so
// the linker can override it; the "dev" literal is the local-build fallback,
// and resolveVersion turns it into the honest version for every provenance.
var buildVersion = "dev"

// exitError carries a process exit code out of a command's RunE. The command
// has already reported the outcome to the user, so Execute does not print it
// again.
type exitError struct {
	code int
}

// Error implements error.
func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

// Execute runs the smith root command and returns the process exit code.
func Execute() int {
	root := newRootCmd()
	return codeFromError(root.Execute())
}

// The exit codes on-box smith answers a staged-config refusal with. They sit
// outside the setup family's 0 pass, 1 partial, 2 gate rejection and 3 connect
// failure, outside the 127 a box with no smith installed answers with, and
// outside relay.RefusalExitCode, so a caller can tell a provisioning gap from a
// document it cannot trust without reading the message. The session verbs
// inherit them.
const (
	exitStagedBlueprintAbsent    = 4
	exitStagedBlueprintMalformed = 5
)

// codeFromError maps a command error to a process exit code. A nil error is 0,
// an exitError carries its own already-reported code, a refused relay carries
// the code the relaying smith classifies as a version mismatch, a staged-config
// refusal
// carries the code its kind is owed however deeply it is wrapped, and any other
// error is a general failure (1).
func codeFromError(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	fmt.Fprintln(os.Stderr, "smith:", err)

	var refused *relayRefusedError
	if errors.As(err, &refused) {
		return relay.RefusalExitCode
	}

	var absent *staging.AbsentError
	if errors.As(err, &absent) {
		return exitStagedBlueprintAbsent
	}
	var malformed *staging.MalformedError
	if errors.As(err, &malformed) {
		return exitStagedBlueprintMalformed
	}
	return 1
}

// newRootCmd builds the root command tree. Usage and error printing are
// silenced so the cli owns all user-facing output and exit codes.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "smith",
		Short:         "Provision a fresh VPS and run coding agents on it",
		Version:       resolveVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// --relayed-from is what the relay always passes and only the relay
	// passes: hidden, because an operator never types it, and optional,
	// because the operator who SSHed in and ran a verb by hand is not
	// relaying and has nothing to declare. It is compared against this
	// binary's own version below.
	var relayedFrom string
	root.PersistentFlags().StringVar(&relayedFrom, "relayed-from", "", "the version of the smith relaying this command")
	if err := root.PersistentFlags().MarkHidden("relayed-from"); err != nil {
		// The flag was registered on the line above, so a failure here is
		// not a runtime condition but a build that cannot be correct.
		panic(fmt.Sprintf("hide --relayed-from: %v", err))
	}
	// Every verb inherits the check, before any of them run: a command line
	// this smith may not mean the same thing by is refused rather than
	// half-understood.
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		return acceptRelayedFrom(resolveVersion(), relayedFrom)
	}
	root.AddCommand(newBlueprintCmd(userConfigHome), newMachineCmd(userConfigHome, connection.System(), connection.SystemDialer(), provider.SystemClock()), newSessionCmd(sessionWiring{
		box:     stagedBoxConfig,
		home:    userConfigHome,
		root:    staging.Root,
		git:     connection.System(),
		tmux:    connection.System(),
		connect: connection.System(),
		ssh:     connection.System(),
		version: resolveVersion(),
		skew:    operatorSkew(),
	}), newWorkspaceCmd(workspaceWiring{
		blueprint: stagedBlueprint,
		home:      userConfigHome,
		command:   connection.System(),
		root:      staging.Root,
		boxHome:   boxHomeDir,
		ssh:       connection.System(),
		version:   resolveVersion(),
		skew:      operatorSkew(),
	}), newVersionCmd())
	return root
}

// operatorSkew is the reaction to a refused relay as the operator's own machine
// makes it: their terminal, and the install stage `machine upgrade` runs.
func operatorSkew() skew {
	return skew{
		term:     secret.NewStdTerminal(),
		converge: convergeBox(userConfigHome, connection.System()),
	}
}
