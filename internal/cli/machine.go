package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byran/smith/internal/bootstrap"
	"github.com/byran/smith/internal/connection"
)

// newMachineCmd builds `smith machine` and its subcommands.
func newMachineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Set up and inspect a remote development box",
	}
	cmd.AddCommand(newSetupCmd(), newStatusCmd())
	return cmd
}

// newSetupCmd builds `smith machine setup <login>@<host>`. For the walking
// skeleton it connects, runs the preflight gate, and reports the outcome
// without mutating the box.
func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup <login>@<host>",
		Short: "Provision, secure, and make a fresh box reachable (preflight only for now)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseTarget(args[0])
			if err != nil {
				return err
			}

			conn := connection.New(target, connection.System())
			res, err := bootstrap.NewRunner(conn).Preflight(cmd.Context())
			if err != nil {
				return fmt.Errorf("preflight: %w", err)
			}

			if _, err := fmt.Fprint(cmd.OutOrStdout(), res.Report()); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if res.Outcome != bootstrap.OutcomePassed {
				return &exitError{code: res.Outcome.ExitCode()}
			}
			return nil
		},
	}
}

// newStatusCmd builds `smith machine status <host>`. It is a stub until the
// drift reconciler lands in a later issue.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <host>",
		Short: "Report how a box has drifted from what setup established (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("machine status is not yet implemented")
		},
	}
}

// parseTarget validates a bootstrap-login target of the form <login>@<host> and
// returns it unchanged for use as the ssh destination. Both the login and host
// must be non-empty.
func parseTarget(arg string) (string, error) {
	login, host, ok := strings.Cut(arg, "@")
	if !ok || login == "" || host == "" {
		return "", fmt.Errorf("invalid target %q: want <login>@<host>", arg)
	}
	return arg, nil
}
