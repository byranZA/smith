package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/provider"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/status"
	"github.com/byranZA/smith/internal/tailscale"
)

// newMachineCmd builds `smith machine` and its subcommands, reading the config
// home through resolve, launching both the provider CLIs and the ssh binary
// through exec, probing a new box's ssh port through dialer, and timing the
// create command's polls by clock.
func newMachineCmd(resolve homeResolver, exec connection.Exec, dialer connection.Dialer, clock provider.Clock) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "machine",
		Short: "Create, set up and inspect a remote development box",
	}
	cmd.AddCommand(
		newCreateCmd(resolve, exec, dialer, clock),
		newSetupCmd(resolve, exec),
		newStatusCmd(resolve, exec),
		newListCmd(resolve),
	)
	return cmd
}

// newCreateCmd builds `smith machine create <name> --blueprint <name-or-path>`.
// It renders the operator's own create template with the box name substituted,
// runs it through the provider CLI the adapter names, and reports the box the
// provider returned.
//
// It stops at "the box exists": nothing is written to the box and no bootstrap
// runs, so the operator is handed the `machine setup` line to run next rather
// than a half-provisioned box. That is deliberate — a chained setup failing
// halfway would leave a box that exists, is billed, and that smith cannot tear
// down, since v1 ships no destroy.
//
// A provider that assigns the address after the create returns is waited out on
// clock, so the operator sees one command whether the address came back with
// the box or a moment later. Once the address is known the box's ssh port is
// polled until it answers, because no provider CLI waits for sshd: a create
// that returned the moment the provider did would hand the operator a setup
// line that is refused for the next half minute.
func newCreateCmd(resolve homeResolver, runner provider.Runner, dialer connection.Dialer, clock provider.Clock) *cobra.Command {
	var blueprintName string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a box at the provider the adapter describes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			adapter, err := resolveAdapter(home, blueprintName)
			if err != nil {
				return reportInvalid(cmd, err)
			}
			box, err := provider.Create(cmd.Context(), runner, clock, adapter, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if err := awaitReachable(cmd.Context(), dialer, clock, box); err != nil {
				return reportInvalid(cmd, err)
			}
			return writeCreated(cmd, box, adapter.SSHKey)
		},
	}
	cmd.Flags().StringVar(&blueprintName, "blueprint", "",
		"the blueprint whose provider adapter creates the box; omitted, the adapter comes from preferences")
	return cmd
}

// readinessTimeout is how long create waits for a new box's ssh port to answer.
// A box that boots normally answers well inside it; a box that does not is one
// the operator has to look at themselves, and waiting longer only delays that.
const readinessTimeout = 5 * time.Minute

// awaitReachable polls the box's ssh port until it answers, and names the box
// when it does not. The box exists and is being billed whatever the poll does,
// and v1 ships no destroy, so a failure that lost the id and the address would
// leave the operator hunting for a box smith made.
func awaitReachable(ctx context.Context, dialer connection.Dialer, clock connection.Clock, box provider.Box) error {
	if err := connection.WaitForSSH(ctx, dialer, clock, box.IP, readinessTimeout); err != nil {
		return fmt.Errorf("box %s was created at %s but never became reachable: %w", box.ID, box.IP, err)
	}
	return nil
}

// resolveAdapter reads the adapter smith would create through, from the named
// blueprint and the operator's preferences. The provider block replaces
// wholesale rather than merging, so what comes back is one coherent adapter.
//
// An adapter is optional everywhere, so its absence is a refusal naming the
// alternative: the operator brings their own box and hands smith the target.
func resolveAdapter(home config.Home, blueprintName string) (provider.Adapter, error) {
	prefs, err := config.LoadPreferences(home)
	if err != nil {
		return provider.Adapter{}, fmt.Errorf("read preferences: %w", err)
	}
	var declared *blueprint.Blueprint
	if blueprintName != "" {
		b, _, err := config.Load(home, blueprintName)
		if err != nil {
			return provider.Adapter{}, fmt.Errorf("read blueprint: %w", err)
		}
		declared = &b
	}
	resolved := config.Resolve(config.Overrides{}, declared, &prefs.Declared)
	if err := resolved.Conflicts(); err != nil {
		return provider.Adapter{}, fmt.Errorf("refusing to create a box: %w", err)
	}
	if resolved.Provider.Provider == nil {
		return provider.Adapter{}, errors.New(
			"no provider adapter is configured: declare a provider block in the blueprint or in preferences, " +
				"or create the box yourself and run smith machine setup <login>@<host>")
	}
	return provider.Adapt(*resolved.Provider.Provider), nil
}

// writeCreated reports the box the provider returned and the command to run
// next.
func writeCreated(cmd *cobra.Command, box provider.Box, sshKey string) error {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), createdReport(box, sshKey)); err != nil {
		return fmt.Errorf("write created report: %w", err)
	}
	return nil
}

// createdReport renders what the operator is handed by a successful create: the
// box, the ssh key reference smith passed to the provider, the command to run
// next, and what to check first when that command cannot get in. The login is
// the one a stock cloud image boots with; an image that grants a different one
// takes that login instead.
//
// The key reference is reported because no pre-flight can validate it. A key
// reference that names nothing, a key registered under another name, and a
// token without permission to read keys are indistinguishable to smith and
// identical to the operator: the box boots, port 22 answers, and the first ssh
// returns Permission denied (publickey). Naming the reference here is what
// turns that into a pointed failure.
func createdReport(box provider.Box, sshKey string) string {
	passed := fmt.Sprintf("%q", sshKey)
	advice := fmt.Sprintf(`If that is refused with "Permission denied (publickey)", the ssh key reference
is the first thing to check. smith passed %q to the provider without
interpreting it, so it cannot tell a wrong reference from a token that may not
read keys.`, sshKey)
	if sshKey == "" {
		passed = "none passed"
		advice = `If that is refused with "Permission denied (publickey)", the ssh key is the
first thing to check. smith passed none, so the box carries only the keys the
provider put on it itself.`
	}
	return fmt.Sprintf(`box created: %s
address:     %s
ssh key:     %s
reachable:   port 22 is answering

nothing is provisioned yet. Run:
  smith machine setup root@%s

%s
`, box.ID, box.IP, passed, box.IP, advice)
}

// newSetupCmd builds `smith machine setup <login>@<host>`. It connects, runs the
// preflight gate, and on a pass runs the ordered mutating phases, streaming their
// live progress. --access selects the access layer: public (default) leaves
// hardened SSH open on the public IP; tailscale joins the box to the operator's
// tailnet as a tag:smith node and closes public SSH once a live tailnet probe
// proves reach. --blueprint names the blueprint the box is built from, read
// through the config home resolve locates and staged onto the box.
func newSetupCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	var accessMode, authKeyRef, blueprintName string
	cmd := &cobra.Command{
		Use:   "setup <login>@<host>",
		Short: "Provision, secure, and make a fresh box reachable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inv, err := lookupInventory(resolve, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			target, err := resolveSetupTarget(inv, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if accessMode != "public" && accessMode != "tailscale" {
				return fmt.Errorf("invalid --access %q: want public or tailscale", accessMode)
			}
			host := hostOf(target)
			ctx := cmd.Context()
			// The config home is read only when there is a blueprint to stage, so
			// an operator with no config home can still set a box up.
			var home config.Home
			if blueprintName != "" {
				h, err := resolve()
				if err != nil {
					return err
				}
				home = h
			}
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

			conn := connection.New(target, exec)
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

			// The blueprint pointer, checked once the box is known reachable and
			// before the first mutating phase: a box built from a blueprint is
			// only ever re-run with one.
			if err := checkBlueprintPointer(ctx, conn, blueprintName, stderr); err != nil {
				return err
			}

			// Every placement source is resolved on the operator's machine before
			// the first mutating phase, because staging is
			// resolve-all-then-write: a reference that will not resolve refuses
			// the run with the box untouched, rather than landing a base layer
			// the operator then has to discover is missing its credentials.
			staged, err := resolveStagedConfig(home, blueprintName, stderr)
			if err != nil {
				return err
			}

			publicSSH, err := publicSSHTarget(ctx, runner, accessMode)
			if err != nil {
				return fmt.Errorf("derive firewall target: %w", err)
			}

			opts := bootstrap.SetupOptions{
				AccessMode:   accessMode,
				SmithVersion: resolveVersion(),
				Blueprint:    blueprintName,
				PublicSSH:    publicSSH,
			}
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

			// The config-staging stage: the base layer is in place, so the box can
			// be told what kind of box it is. It runs before the access layer
			// closes any door smith is still reached over.
			if err := stageConfig(ctx, conn, staged, stdout); err != nil {
				if _, werr := fmt.Fprintf(stderr, "config staging failed: %v\n", err); werr != nil {
					return fmt.Errorf("write staging failure: %w", werr)
				}
				return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
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
	cmd.Flags().StringVar(&blueprintName, "blueprint", "",
		"the blueprint the box is built from, staged onto it; omitted, nothing is staged")
	return cmd
}

// checkBlueprintPointer applies the blueprint-pointer rule at the command
// surface: a run naming no blueprint against a box whose marker records one is
// refused, naming the blueprint the box was built from, and exits as a gate
// rejection — the code the setup family already answers a refusal that mutated
// nothing with.
//
// It runs after the preflight gate — which mutates nothing and owns the
// connect-failure outcome — and before the first mutating phase, so a refusal
// leaves the box, its staged document and its staged placements exactly as they
// were.
func checkBlueprintPointer(ctx context.Context, conn staging.Conn, blueprintName string, stderr io.Writer) error {
	if err := staging.CheckPointer(ctx, conn, blueprintName); err != nil {
		return refuseSetup(stderr, err)
	}
	return nil
}

// stagedConfig is the operator's blueprint resolved and ready to go onto the
// box: the tree the staging stage converges, and the document it was read from,
// named when a write fails so the operator knows which blueprint the box choked
// on.
type stagedConfig struct {
	tree staging.Tree
	path string
}

// resolveStagedConfig reads the named blueprint from the operator's config home
// and resolves every placement source on the operator's machine. It takes no
// connection and reaches no box, so a refusal here cannot have created or
// modified a byte of /etc/smith.
//
// It runs before the first mutating phase because staging is
// resolve-all-then-write: a from: reference naming a path or a variable this
// machine does not have is a blueprint the operator has to fix, and finding
// that out after the base layer landed would leave a provisioned box configured
// for a session that cannot run. Every unresolvable reference is enumerated in
// one refusal, so a blueprint full of typos is fixed in one pass rather than
// one run per typo, and the refusal exits as a gate rejection -- the code the
// setup family answers a refusal that mutated nothing with.
//
// A run naming no blueprint resolves nothing and stages nothing: the box keeps
// whatever it already holds, and the flag-only path survives.
func resolveStagedConfig(home config.Home, blueprintName string, stderr io.Writer) (*stagedConfig, error) {
	if blueprintName == "" {
		return nil, nil
	}
	doc, err := config.LoadDocument(home, blueprintName)
	if err != nil {
		return nil, refuseSetup(stderr, fmt.Errorf("read blueprint: %w", err))
	}
	tree, err := staging.Resolve(staging.Plan(doc.Bytes, doc.Blueprint), secret.Resolve)
	if err != nil {
		return nil, refuseSetup(stderr, fmt.Errorf("stage blueprint %s: %w", doc.Path, err))
	}
	return &stagedConfig{tree: tree, path: doc.Path}, nil
}

// refuseSetup reports a setup refusal that mutated nothing and exits as a gate
// rejection, the code the setup family already answers such a refusal with.
func refuseSetup(stderr io.Writer, err error) error {
	if _, werr := fmt.Fprintf(stderr, "setup refused: %v\n", err); werr != nil {
		return fmt.Errorf("write refusal: %w", werr)
	}
	return &exitError{code: bootstrap.OutcomeRejected.ExitCode()}
}

// stageConfig runs the config-staging stage: it writes the resolved blueprint
// and its placement bytes onto the box at /etc/smith/, reporting what it staged
// and what it left alone.
//
// It converges rather than resolves: the document was parsed and every source
// resolved before the box was touched, so the only failures left here are the
// box's own. It runs after the base layer because the staged placements are
// owned by the smith user, who does not exist until then. Nothing to stage --
// a run naming no blueprint -- leaves the box exactly as it was.
func stageConfig(ctx context.Context, conn staging.Conn, staged *stagedConfig, stdout io.Writer) error {
	if staged == nil {
		return nil
	}
	result, err := staging.Converge(ctx, conn, staged.tree)
	if err != nil {
		return fmt.Errorf("stage blueprint %s: %w", staged.path, err)
	}
	if _, err := fmt.Fprint(stdout, result.Report()); err != nil {
		return fmt.Errorf("write staging report: %w", err)
	}
	return nil
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

// newListCmd builds `smith machine list`. It reads the box inventory out of the
// config home and prints what it finds, sorted by name with a summary line.
//
// It is instant and offline: nothing is connected to, no command is run on any
// box, and no file or directory is created — a box knows of no other boxes, so
// there is nothing out there to ask. An absent inventory is not a failure but
// the ordinary state before the first box is registered, so it reports that and
// exits 0; only a file smith cannot read is an error, and that error names the
// file rather than being papered over as empty.
func newListCmd(resolve homeResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the boxes smith knows how to reach",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			inv, skew, err := inventory.Read(home.InventoryPath())
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), inventory.Readout(inv, skew)); err != nil {
				return fmt.Errorf("write box listing: %w", err)
			}
			return nil
		},
	}
}

// newStatusCmd builds `smith machine status <name-or-target>`. It resolves the
// argument through the shared rule — the "@" decides — connects over what that
// yields, gathers the box's live facts read-only, reconciles them against the
// marker, prints the drift report, and maps the verdict to an exit code (0
// matches, 1 drifted, 2 not-provisioned, 3 unreachable). It never mutates the
// box — the marker's access_mode, not a flag, sets probe expectations, so there
// is deliberately no --access flag.
//
// A refusal on a bare argument smith did not recognise carries the two fixes
// with it: status no longer has a private convention that turns a bare address
// into the smith user, so an operator who relied on that one is told the
// address to pass and the command that registers the box under a name.
func newStatusCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	return &cobra.Command{
		Use:   "status <name-or-target>",
		Short: "Report how a box has drifted from what setup established",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inv, err := lookupInventory(resolve, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			target := inventory.Resolve(inv, args[0])
			ctx := cmd.Context()

			conn := connection.New(target, exec)
			admin := tailscale.NewAdmin(connection.System())
			prober := status.NewProber(conn, admin)

			gathered, err := prober.Gather(ctx)
			if err != nil {
				return fmt.Errorf("probe box: %w", err)
			}

			report := status.Unreachable(target)
			if gathered.Reachable {
				report = status.Reconcile(gathered.Marker, gathered.Skew, gathered.MarkerPresent, gathered.Facts)
			}
			out := report.String()
			if !gathered.Reachable {
				out += inventory.ConnectHint(inv, args[0])
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), out); err != nil {
				return fmt.Errorf("write status report: %w", err)
			}
			if code := report.ExitCode(); code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
}

// lookupInventory reads the box inventory a verb resolves its argument
// against, and reads nothing at all when the argument contains "@": a literal
// target is never looked up, so an unreadable inventory cannot stand between
// the operator and a box they addressed directly.
func lookupInventory(resolve homeResolver, arg string) (inventory.Inventory, error) {
	if strings.Contains(arg, "@") {
		return inventory.Empty(), nil
	}
	home, err := resolve()
	if err != nil {
		return inventory.Empty(), err
	}
	inv, _, err := inventory.Read(home.InventoryPath())
	if err != nil {
		return inventory.Empty(), err
	}
	return inv, nil
}

// resolveSetupTarget resolves setup's argument under the shared rule and holds
// it to the shape setup needs. Setup is the one verb that reads the target's
// host — the tailnet prerequisites and the firewall phase are written in terms
// of it — so an opaque target it cannot take apart is refused here rather than
// half-applied later. An argument that resolves to nothing keeps the refusal it
// has always had, which already names the shape to pass.
func resolveSetupTarget(inv inventory.Inventory, arg string) (string, error) {
	resolved := inventory.Resolve(inv, arg)
	target, err := parseTarget(resolved)
	if err == nil {
		return target, nil
	}
	if resolved != arg {
		return "", fmt.Errorf("box %q is registered as %q, which setup cannot use: want <login>@<host>", arg, resolved)
	}
	return "", err
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
