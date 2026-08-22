package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/provider"
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
		newListCmd(resolve, exec),
		newAddCmd(resolve, exec),
		newForgetCmd(resolve),
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

// newListCmd builds `smith machine list [--probe]`. It reads the box inventory
// out of the config home and prints what it finds, sorted by name with a
// summary line.
//
// Without --probe it is instant and offline: nothing is connected to, no
// command is run on any box, and no file or directory is created — a box knows
// of no other boxes, so there is nothing out there to ask. An absent inventory
// is not a failure but the ordinary state before the first box is registered,
// so it reports that and exits 0; only a file smith cannot read is an error,
// and that error names the file rather than being papered over as empty.
//
// With --probe it opens a connection to every box at once and adds the
// REACHABLE column. That is the whole of the difference: the probe never
// writes, never prunes and never fails the command, because reachability is a
// display concern and an entry smith could not reach is at least as often
// rebooting, off-tailnet or firewalled as it is gone — and v1 has no way to
// fetch a pruned entry back.
func newListCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	var probe bool
	cmd := &cobra.Command{
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
			var reach map[string]bool
			if probe {
				if reach, err = probeBoxes(cmd.Context(), inv, exec); err != nil {
					return reportInvalid(cmd, err)
				}
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), inventory.Readout(inv, skew, reach)); err != nil {
				return fmt.Errorf("write box listing: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&probe, "probe", false,
		"connect to every listed box and report whether it answers; nothing is stored and no entry is pruned")
	return cmd
}

// probeBoxes reports which of the inventory's boxes answer, opening one
// connection per box over its registered target. The targets are used
// verbatim, as everywhere: they are what smith proved, so there is nothing to
// resolve them against.
func probeBoxes(ctx context.Context, inv inventory.Inventory, exec connection.Exec) (map[string]bool, error) {
	conns := make(map[string]status.Conn, len(inv.Boxes))
	for name, box := range inv.Boxes {
		conns[name] = connection.New(box.Target, exec)
	}
	reach, err := status.ProbeAll(ctx, conns)
	if err != nil {
		return nil, fmt.Errorf("probe listed boxes: %w", err)
	}
	return reach, nil
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

// lookupInventory locates the config home and returns the inventory a verb
// resolves arg against, leaving to the inventory package whether the file is
// read at all.
func lookupInventory(resolve homeResolver, arg string) (inventory.Inventory, error) {
	home, err := resolve()
	if err != nil {
		return inventory.Empty(), err
	}
	inv, _, err := inventory.LoadFor(home.InventoryPath(), arg)
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

// newAddCmd builds `smith machine add <target> [--name <name>]`. It registers a
// box smith already provisioned — the manual reconstruction path for an
// inventory that was deleted, and the way a second machine learns the boxes the
// first one set up.
//
// Registration is read-only towards the box: smith connects, reads the marker,
// and registers or refuses. It never writes a marker, because a box carrying
// none is not an unregistered smith box — it is a box smith has never
// provisioned, and stamping one would assert provisioned, secured and reachable
// state that does not exist. The refusal names `machine setup` instead.
//
// The argument is the target, used verbatim: add is registering an address, so
// there is nothing to resolve it against yet. The name is the operator's
// --name, or the box's host when they pass none — a marker recording a name is
// the marker-v2 slice's, and until then smith says which fallback it used
// rather than letting the operator assume the box named itself.
func newAddCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "add <target>",
		Short: "Register a box smith already provisioned",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			ctx := cmd.Context()
			conn := connection.New(target, exec)

			reachable, err := status.Reachable(ctx, conn)
			if err != nil {
				return fmt.Errorf("probe box: %w", err)
			}
			if !reachable {
				return refuseAdd(cmd, bootstrap.OutcomeConnectFailed,
					fmt.Errorf("could not connect to %s: register it once smith can reach it", target))
			}

			box, present, err := status.ReadMarker(ctx, conn)
			if err != nil {
				return fmt.Errorf("read marker: %w", err)
			}
			if !present {
				return refuseAdd(cmd, bootstrap.OutcomeRejected, fmt.Errorf(
					"%s has never been set up by smith: it carries no marker at %s.\n"+
						"Provision it first with:\n  smith machine setup <login>@%s",
					target, marker.Path, hostOrTarget(target)))
			}

			home, err := resolve()
			if err != nil {
				return err
			}
			registered, origin := inventory.Name(inventory.Naming{
				Flag:   name,
				Marker: box.Name,
				Host:   hostOrTarget(target),
			})
			if _, err := registerBox(home, registration{name: registered, target: target}); err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), addedReport(registered, target, origin, box.Blueprint)); err != nil {
				return fmt.Errorf("write registration: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the name to register the box under; omitted, its host names it")
	return cmd
}

// registration is one box's registration as the verb running it knows it: the
// name to register, the target to register it under, the name the box's own
// marker records, and the address this run reached the box over. The last two
// are what decide whether the box already has an entry to move, and a verb with
// nothing to move — `machine add`, which reads a marker it never wrote — leaves
// them empty.
type registration struct {
	// recorded is the name the box's marker records, empty when the caller has
	// no recorded name to move an entry off.
	recorded string
	// addressed is the address this run reached the box over, which is how a
	// re-run of a registered box is told from a box carrying another box's name.
	addressed string
	// name is the name the box is registered under.
	name string
	// target is the proven address the name maps to.
	target string
}

// registerBox reads the inventory, maps r.name to r.target in it, and writes it
// back. It returns the name whose entry moved, empty when nothing moved, so the
// caller reports a rename only when there was one. The name is decided by the
// caller: each verb derives its own default from what it knows, and this one
// only ever registers the name it is handed.
//
// A registration is a rename when the box already has an entry under the name
// its marker records: the old entry goes, so renaming a box leaves one entry
// rather than two names for one machine, and a re-run whose address changed
// updates the entry it already has. Whether that entry is this box's is the
// inventory's call, not the marker's — see inventory.PreviousName.
//
// The config home is created here and only here on the registration paths:
// reading never creates, so the directory comes into existence at the first
// write into it.
func registerBox(home config.Home, r registration) (string, error) {
	inv, skew, err := inventory.Read(home.InventoryPath())
	if err != nil {
		return "", fmt.Errorf("read the box inventory: %w", err)
	}
	previous := inventory.PreviousName(inv, r.recorded, r.addressed, r.target)
	next, err := inventory.Rename(inv, previous, r.name, r.target)
	if err != nil {
		return "", fmt.Errorf("cannot register %s: %w", r.target, err)
	}
	if err := config.EnsureHome(home); err != nil {
		return "", fmt.Errorf("create the config home: %w", err)
	}
	if err := inventory.Write(home.InventoryPath(), next, skew); err != nil {
		return "", fmt.Errorf("register %s: %w", r.target, err)
	}
	if previous == r.name {
		return "", nil
	}
	return previous, nil
}

// addedReport renders what the operator is told by a successful registration:
// the name the box is reached by from now on, the target it resolves to, and —
// when the name came off nothing better than the host — that the box's marker
// recorded none.
func addedReport(name, target string, origin inventory.Origin, blueprint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "registered %s as %q\n", target, name)
	if blueprint != "" {
		fmt.Fprintf(&b, "built from the blueprint %q\n", blueprint)
	}
	if origin == inventory.OriginHost {
		fmt.Fprintf(&b, "\nthe box's marker records no name, so smith named it after its host.\n"+
			"Register it under another name with: smith machine add %s --name <name>\n", target)
	}
	return b.String()
}

// refuseAdd reports a registration smith declined and exits with the code that
// kind of refusal is owed: a box that never answered is a connect failure, and
// one that answered but carries no marker is a gate rejection. Neither wrote
// anything, on the box or in the inventory.
func refuseAdd(cmd *cobra.Command, outcome bootstrap.Outcome, cause error) error {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "add refused: %v\n", cause); err != nil {
		return fmt.Errorf("write refusal: %w", err)
	}
	return &exitError{code: outcome.ExitCode()}
}

// hostOrTarget returns the host part of a <login>@<host> target, or the whole
// value when it carries no login — an ssh_config alias is its own host, and is
// as good a name for the box as anything smith could invent.
func hostOrTarget(target string) string {
	if _, host, ok := strings.Cut(target, "@"); ok && host != "" {
		return host
	}
	return target
}

// newForgetCmd builds `smith machine forget <name>`. It removes one entry from
// the box inventory and does nothing else: no connection is opened and the box
// is not touched, so a forgotten box keeps running and can be registered again
// with `machine add` the moment the operator wants it back. That is the whole
// safety story — forgetting costs a line in a file, never a machine.
//
// The argument is a registered name, never a target: a value smith would hand
// straight to ssh names nothing in the inventory, so there would be nothing to
// remove. A name no box is registered under is a refusal rather than a silent
// success, because an operator who mistypes a name is owed the news that the
// entry they meant is still there.
func newForgetCmd(resolve homeResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <name>",
		Short: "Remove a box from the inventory, leaving the box itself alone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := resolve()
			if err != nil {
				return err
			}
			target, err := forgetBox(home, args[0])
			if err != nil {
				return reportInvalid(cmd, err)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), forgottenReport(args[0], target)); err != nil {
				return fmt.Errorf("write removal: %w", err)
			}
			return nil
		},
	}
}

// forgetBox reads the inventory, removes name from it, and writes it back. It
// returns the target the name reached, which is what the operator needs to put
// the entry back. The config home is never created here: forgetting a name that
// is not registered writes nothing, and a name cannot be registered in an
// inventory that does not exist.
func forgetBox(home config.Home, name string) (string, error) {
	inv, skew, err := inventory.Read(home.InventoryPath())
	if err != nil {
		return "", fmt.Errorf("read the box inventory: %w", err)
	}
	next, err := inventory.Forget(inv, name)
	if err != nil {
		return "", fmt.Errorf("cannot forget %s: %w", name, err)
	}
	if err := inventory.Write(home.InventoryPath(), next, skew); err != nil {
		return "", fmt.Errorf("forget %s: %w", name, err)
	}
	return inv.Boxes[name].Target, nil
}

// forgottenReport renders what the operator is told by a successful removal:
// the name that no longer reaches anything, and the command that registers the
// box again — the entry is gone, and smith is the only thing that knew where
// the box was.
func forgottenReport(name, target string) string {
	return fmt.Sprintf("forgot %q (%s)\n\nRegister it again with:\n  smith machine add %s --name %s\n",
		name, target, target, name)
}
