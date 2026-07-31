package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/status"
	"github.com/byranZA/smith/internal/tailscale"
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

// newSetupCmd builds `smith machine setup <login>@<host>`. It connects, runs the
// preflight gate, and on a pass runs the ordered mutating phases, streaming their
// live progress. --access selects the access layer: public (default) leaves
// hardened SSH open on the public IP; tailscale joins the box to the operator's
// tailnet as a tag:smith node and closes public SSH once a live tailnet probe
// proves reach.
func newSetupCmd() *cobra.Command {
	var accessMode, authKeyRef string
	cmd := &cobra.Command{
		Use:   "setup <login>@<host>",
		Short: "Provision, secure, and make a fresh box reachable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := parseTarget(args[0])
			if err != nil {
				return err
			}
			if accessMode != "public" && accessMode != "tailscale" {
				return fmt.Errorf("invalid --access %q: want public or tailscale", accessMode)
			}
			host := hostOf(target)
			ctx := cmd.Context()
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			conn := connection.New(target, connection.System())
			runner := bootstrap.NewRunner(conn)

			// Tailscale up-front, before anything on the box is mutated: acquire the
			// auth key, refuse if this admin machine is not itself on the tailnet
			// (smith could not then verify reach), and print the one-time tailnet
			// prerequisites personalized to the operator.
			var access *tailscale.Access
			var acquireKey func() (string, error)
			if accessMode == "tailscale" {
				a, acq, err := prepareTailscale(ctx, conn, host, authKeyRef, stdout)
				if err != nil {
					if _, werr := fmt.Fprintf(stderr, "setup refused: %v\n", err); werr != nil {
						return fmt.Errorf("write refusal: %w", werr)
					}
					return &exitError{code: bootstrap.OutcomeRejected.ExitCode()}
				}
				access, acquireKey = a, acq
			}

			res, err := runner.Preflight(ctx)
			if err != nil {
				return fmt.Errorf("preflight: %w", err)
			}
			if _, err := fmt.Fprint(stdout, res.Report()); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			if res.Outcome != bootstrap.OutcomePassed {
				return &exitError{code: res.Outcome.ExitCode()}
			}

			publicSSH, err := publicSSHTarget(ctx, runner, accessMode)
			if err != nil {
				return fmt.Errorf("derive firewall target: %w", err)
			}

			opts := bootstrap.SetupOptions{AccessMode: accessMode, SmithVersion: resolveVersion(), PublicSSH: publicSSH}
			setupRes, err := runner.Setup(ctx, opts, stdout, stderr)
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

			if accessMode == "tailscale" {
				return establishTailscale(ctx, access, host, acquireKey, stdout, stderr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&accessMode, "access", "public", "how the box is reached: public or tailscale")
	cmd.Flags().StringVar(&authKeyRef, "tailscale-auth-key", "",
		"reference to the Tailscale auth key for --access=tailscale (env:VAR or file:/path); prompts if omitted on a terminal")
	return cmd
}

// prepareTailscale performs the up-front, no-mutation tailscale steps: it
// refuses when the admin machine is not on the tailnet, fails fast when a key
// would be needed but there is nothing to prompt and no reference to resolve,
// and prints the two one-time-per-tailnet prerequisites personalized to the
// operator. It returns the Access orchestrator and a key-acquiring closure the
// enroll step calls only if the box actually needs enrolling — so a re-run of an
// already-reachable box never resolves (or prompts for) a fresh auth key.
func prepareTailscale(ctx context.Context, conn *connection.SSH, host, authKeyRef string, stdout io.Writer) (*tailscale.Access, func() (string, error), error) {
	// Fail fast before mutating anything when the key could never be obtained:
	// no reference to resolve and no interactive terminal to prompt. Acquisition
	// itself is deferred to enroll, so an already-satisfied re-run needs no key.
	term := secret.NewStdTerminal()
	if authKeyRef == "" && !term.Interactive() {
		return nil, nil, fmt.Errorf("resolve tailscale auth key: %w", secret.ErrNoReference)
	}

	admin := tailscale.NewAdmin(connection.System())
	if err := tailscale.CheckAdminOnTailnet(ctx, admin); err != nil {
		return nil, nil, fmt.Errorf("tailnet preflight: %w", err)
	}

	status, err := admin.Status(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read tailnet identity: %w", err)
	}
	if _, err := fmt.Fprint(stdout, tailscale.Prereqs(status.Identity, host)); err != nil {
		return nil, nil, fmt.Errorf("write prerequisites: %w", err)
	}

	box := tailscale.NewBox(conn, bootstrap.RemoteScriptPath)
	acquireKey := func() (string, error) {
		return secret.Acquire(authKeyRef, "Tailscale auth key: ", term)
	}
	return tailscale.NewAccess(box, admin), acquireKey, nil
}

// publicSSHTarget derives the access-aware firewall target for public port 22:
// public mode keeps it open, and tailscale mode keeps it open while smith is
// still reached over public SSH but closed on a re-run reached over the tailnet,
// so a tailnet re-run never transiently re-opens public 22.
func publicSSHTarget(ctx context.Context, runner *bootstrap.Runner, accessMode string) (string, error) {
	if accessMode != "tailscale" {
		return tailscale.PublicSSHOpen.String(), nil
	}
	sshConn, err := runner.SSHConnection(ctx)
	if err != nil {
		return "", fmt.Errorf("probe ssh connection: %w", err)
	}
	over := tailscale.ConnectedOverTailnet(sshConn)
	return tailscale.PublicSSHTarget(accessMode, over).String(), nil
}

// establishTailscale runs the probe-gated tailscale access sequence after the
// base layer is in place: enroll the tag:smith node, prove reach with a live
// tailnet ssh probe, and only then close public SSH. A failure leaves public SSH
// open and reports which prerequisite to fix (exit 1, partial but reachable). On
// success it tells the operator the tailnet name to re-run over; a box already
// enrolled and reachable is reported as an already-satisfied no-op.
func establishTailscale(ctx context.Context, access *tailscale.Access, host string, acquireKey func() (string, error), stdout, stderr io.Writer) error {
	result, err := access.Establish(ctx, tailscale.EstablishOptions{Host: host, AcquireKey: acquireKey})
	if err != nil {
		if _, werr := fmt.Fprintf(stderr, "tailscale access not established: %v\n", err); werr != nil {
			return fmt.Errorf("write tailscale failure: %w", werr)
		}
		return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
	}
	headline := "tailscale reach established over %s; public SSH closed.\nRe-run over the box's tailnet name: smith machine setup smith@%s\n"
	if result.AlreadySatisfied {
		headline = "tailscale access already satisfied over %s; public SSH already closed.\nRe-run over the box's tailnet name: smith machine setup smith@%s\n"
	}
	if _, err := fmt.Fprintf(stdout, headline, result.TailnetIP, result.ReRunHost); err != nil {
		return fmt.Errorf("write success: %w", err)
	}
	return nil
}

// newStatusCmd builds `smith machine status <host>`. It connects as smith@host
// (the tailnet name in tailscale mode), gathers the box's live facts read-only,
// reconciles them against the marker, prints the drift report, and maps the
// verdict to an exit code (0 matches, 1 drifted, 2 not-provisioned, 3
// unreachable). It never mutates the box — the marker's access_mode, not a flag,
// sets probe expectations, so there is deliberately no --access flag.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <host>",
		Short: "Report how a box has drifted from what setup established",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := parseStatusHost(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			conn := connection.New("smith@"+host, connection.System())
			admin := tailscale.NewAdmin(connection.System())
			prober := status.NewProber(conn, admin)

			gathered, err := prober.Gather(ctx)
			if err != nil {
				return fmt.Errorf("probe box: %w", err)
			}

			report := status.Unreachable(host)
			if gathered.Reachable {
				report = status.Reconcile(gathered.Marker, gathered.Skew, gathered.MarkerPresent, gathered.Facts)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), report.String()); err != nil {
				return fmt.Errorf("write status report: %w", err)
			}
			if code := report.ExitCode(); code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
}

// parseStatusHost validates the status host argument: a bare host (or tailnet
// name), never a <login>@<host> — status always connects as the smith user, so a
// login prefix is a mistake worth catching.
func parseStatusHost(arg string) (string, error) {
	if arg == "" {
		return "", errors.New("invalid host: want a bare <host>")
	}
	if strings.Contains(arg, "@") {
		return "", fmt.Errorf("invalid host %q: pass a bare <host>, not <login>@<host> — status connects as smith", arg)
	}
	return arg, nil
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

// hostOf returns the host part of a validated <login>@<host> target.
func hostOf(target string) string {
	_, host, _ := strings.Cut(target, "@")
	return host
}
