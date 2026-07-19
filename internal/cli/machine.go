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

// newSetupCmd builds `smith machine setup <login>@<host>`. It connects, runs
// the preflight gate, and on a pass runs the ordered mutating phases, streaming
// their live progress. Access mode is public for now; the tailscale access
// layer lands in a later issue.
func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup <login>@<host>",
		Short: "Provision, secure, and make a fresh box reachable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseTarget(args[0])
			if err != nil {
				return err
			}

			conn := connection.New(target, connection.System())
			runner := bootstrap.NewRunner(conn)
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			res, err := runner.Preflight(cmd.Context())
			if err != nil {
				return fmt.Errorf("preflight: %w", err)
			}
			if _, err := fmt.Fprint(stdout, res.Report()); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if res.Outcome != bootstrap.OutcomePassed {
				return &exitError{code: res.Outcome.ExitCode()}
			}

			opts := bootstrap.SetupOptions{AccessMode: "public", SmithVersion: buildVersion}
			setupRes, err := runner.Setup(cmd.Context(), opts, stdout, stderr)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}
			if setupRes.Failure != nil {
				if _, err := fmt.Fprint(stderr, setupRes.Failure.Report()); err != nil {
					return fmt.Errorf("write failure report: %w", err)
				}
			}
			if setupRes.Outcome != bootstrap.OutcomePassed {
				return &exitError{code: setupRes.Outcome.ExitCode()}
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
