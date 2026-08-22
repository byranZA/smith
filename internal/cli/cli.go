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
// failure, and outside the 127 a box with no smith installed answers with, so a
// caller can tell a provisioning gap from a document it cannot trust without
// reading the message. The session verbs inherit them.
const (
	exitStagedBlueprintAbsent    = 4
	exitStagedBlueprintMalformed = 5
)

// codeFromError maps a command error to a process exit code. A nil error is 0,
// an exitError carries its own already-reported code, a staged-config refusal
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
	root.AddCommand(newBlueprintCmd(userConfigHome), newMachineCmd(userConfigHome, connection.System(), connection.SystemDialer(), provider.SystemClock()), newVersionCmd())
	return root
}
