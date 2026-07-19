// Package cli is the cobra command surface for smith. It stays thin: each RunE
// parses flags, calls the domain packages, and formats the result. Process
// exit codes are owned here so main can stay a one-liner.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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

// codeFromError maps a command error to a process exit code. A nil error is 0,
// an exitError carries its own already-reported code, and any other error is a
// general failure (1).
func codeFromError(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	fmt.Fprintln(os.Stderr, "smith:", err)
	return 1
}

// newRootCmd builds the root command tree. Usage and error printing are
// silenced so the cli owns all user-facing output and exit codes.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "smith",
		Short:         "Provision a fresh VPS and run coding agents on it",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newMachineCmd())
	return root
}
