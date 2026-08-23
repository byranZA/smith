package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/relay"
	"github.com/byranZA/smith/internal/secret"
	"github.com/byranZA/smith/internal/staging"
	"github.com/byranZA/smith/internal/status"
	"github.com/byranZA/smith/internal/tailscale"
)

// smithLogin is the login a box smith has provisioned is reached as from now
// on. The bootstrap-in login is dead by the time setup finishes — hardening
// closes root logins — so the ongoing target is the smith user's, and it is
// the one smith proves before writing anything down.
const smithLogin = "smith"

// newSetupCmd builds `smith machine setup <login>@<host>`. It connects, runs the
// preflight gate, and on a pass runs the ordered mutating phases, streaming their
// live progress. --access selects the access layer: public (default) leaves
// hardened SSH open on the public IP; tailscale joins the box to the operator's
// tailnet as a tag:smith node and closes public SSH once a live tailnet probe
// proves reach. --blueprint names the blueprint the box is built from, read
// through the config home resolve locates and staged onto the box. --name names
// the box, stamped onto its marker so the box records what the operator calls
// it and registered in the inventory so they can type it instead of an address.
//
// Once every phase has completed, the run drives the ordered pipeline of named
// stages that sit on top of a provisioned box: the access layer, the smith
// binary, the staged configuration and the workspace. --smith-version names the
// released smith the install stage puts on the box, for a build with no release
// of its own and for an operator pinning a box to an older smith.
//
// A run that gets all the way through ends by proving the target the box is
// reachable by from now on and registering it — the address setup used to print
// once and throw away.
func newSetupCmd(resolve homeResolver, exec connection.Exec) *cobra.Command {
	var accessMode, authKeyRef, blueprintName, boxName, boxTarget, smithVersion string
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
			// The config home is located up front — locating it reads nothing —
			// because a successful setup registers the box in the inventory
			// inside it. It is read only when there is a blueprint to stage, and
			// created only by that registration, so an operator with no config
			// home can still set a box up.
			home, err := resolve()
			if err != nil {
				return err
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
				a, acq, err := prepareTailscale(ctx, conn, exec, host, authKeyRef, stdout)
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

			// The box's own marker, read once the preflight gate says the box
			// answers and before the first mutating phase. It carries both the
			// blueprint the box was built from and the name it is already
			// registered under, so one read settles the pointer rule and the
			// name a re-run inherits.
			recorded, _, err := status.ReadMarker(ctx, conn)
			if err != nil {
				return fmt.Errorf("read the box's marker: %w", err)
			}
			if err := staging.Pointer(recorded, blueprintName); err != nil {
				return refuseSetup(stderr, err)
			}

			// The name, by precedence: what the operator passed, then what the
			// box already records, then the blueprint it is built from, then its
			// host. A --name that differs from the marker's is a rename, and the
			// resolved name goes onto the marker so the box records what it is
			// called.
			names := inventory.Naming{
				Flag:      boxName,
				Marker:    recorded.Name,
				Blueprint: blueprintName,
				Host:      host,
			}
			resolvedName, _ := inventory.Name(names)

			// Every reference the blueprint declares — each placement's source
			// and the value of every variable its env exports, box-scoped and
			// per-repo alike — is resolved on the operator's machine before the
			// first mutating phase, because staging is resolve-all-then-write: a
			// reference that will not resolve refuses the run with the box
			// untouched, rather than landing a base layer the operator then has
			// to discover is missing its credentials.
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
				BoxName:      resolvedName,
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

			// Every phase completed, so what is left is the pipeline: the
			// ordered, named stages that run from the operator's machine on top
			// of a box that is already provisioned and secured.
			run := &pipelineRun{
				accessMode:   accessMode,
				host:         host,
				exec:         exec,
				access:       access,
				acquireKey:   acquireKey,
				box:          args[0],
				smithVersion: chosenVersion(smithVersion, resolveVersion()),
				localVersion: resolveVersion(),
				staged:       staged,
			}
			if err := bootstrap.RunStages(ctx, run.stages(stdout, stderr), stdout); err != nil {
				return reportStageFailure(stderr, err)
			}

			// The box is provisioned; what is left is writing down the address
			// smith will reach it by from now on, which the operator would
			// otherwise have to remember off a line that scrolls away.
			conclusion := setupConclusion{
				accessMode: accessMode,
				tailnetIP:  run.tailnetIP,
				target:     boxTarget,
				addressed:  target,
				names:      names,
			}
			return concludeSetup(ctx, exec, home, conclusion, stdout, stderr)
		},
	}
	cmd.Flags().StringVar(&accessMode, "access", "public", "how the box is reached: public or tailscale")
	cmd.Flags().StringVar(&authKeyRef, "tailscale-auth-key", "",
		"reference to the Tailscale auth key for --access=tailscale (env:VAR or file:/path); prompts if omitted on a terminal")
	cmd.Flags().StringVar(&blueprintName, "blueprint", "",
		"the blueprint the box is built from, staged onto it; omitted, nothing is staged")
	cmd.Flags().StringVar(&boxName, "name", "",
		"the name the box is registered and recorded under; omitted, its marker, its blueprint or its host names it")
	cmd.Flags().StringVar(&smithVersion, "smith-version", "",
		"the released smith version the install stage puts on the box; omitted, the box is converged to the version local smith runs")
	cmd.Flags().StringVar(&boxTarget, "target", "",
		"the address to register the box under, stored verbatim and never probed; omitted, smith registers the address it proved")
	return cmd
}

// stagedConfig is the operator's blueprint resolved and ready to go onto the
// box: the tree the staging stage converges — document, placement bytes and
// resolved env — and the document it was read from,
// named when a write fails so the operator knows which blueprint the box choked
// on.
type stagedConfig struct {
	tree staging.Tree
	path string
}

// resolveStagedConfig reads the named blueprint from the operator's config home
// and resolves every reference it declares on the operator's machine: each
// placement's source, and the value of every variable its env exports, at the
// box scope and inside each repo. It takes no connection and reaches no box, so
// a refusal here cannot have created or modified a byte of /etc/smith.
//
// Both grammars resolve here and neither resolves on the box. env:GH_TOKEN
// names a variable in the operator's shell and file:/home/op/.secrets a path on
// their disk, so what travels is the value and the staged document keeps the
// reference as dead provenance. It runs before the workspace stage is relayed,
// because that stage exports these values through the mise config it generates
// and reads them by name rather than resolving one.
//
// It runs before the first mutating phase because staging is
// resolve-all-then-write: a reference naming a path or a variable this
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
	tree, err := staging.Resolve(staging.Plan(doc.Bytes, doc.Blueprint), secret.Resolve, blueprint.Value)
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

// stageConfig runs the config-staging stage: it writes the staged blueprint,
// its placement bytes and the resolved values of its env onto the box at
// /etc/smith/, reporting what it staged and what it left alone.
//
// It converges rather than resolves: the document was parsed and every
// reference resolved before the box was touched, so the only failures left here
// are the box's own. It runs after the base layer because the staged placements are
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

// convergeWorkspace runs the workspace stage: it relays `smith workspace
// converge` to the box, which reads the blueprint just staged on it and
// converges the box to the workspace that blueprint declares, streaming the
// box's own progress back to the operator's terminal. It reads the values of
// that blueprint's env by name out of what was staged beside the document, and
// resolves no reference of its own: a variable with no staged value refuses the
// step by name rather than falling back to what the box happens to hold.
//
// It relays rather than reimplements, because the stage clones repos and
// installs runtimes onto the box and so has to run there (docs/adr/0008). The
// relay travels as the smith user, the login the box is reached by from now
// on: the bootstrap-in login is dead by this point, since hardening has closed
// it.
//
// A run naming no blueprint converges nothing. There is nothing staged for the
// box to read, and refusing on its absence would break the flag-only path that
// never had a blueprint to begin with.
//
// A stage the box refused is a failed stage, not a failed setup: every phase
// completed and the box is provisioned, secured and configured, so what the
// operator is owed is the pipeline's own partial report — and the box has
// already said what went wrong on their terminal.
func convergeWorkspace(ctx context.Context, exec connection.Exec, target, version string, staged *stagedConfig, stdout, stderr io.Writer) error {
	if staged == nil {
		return nil
	}
	verb := relay.Verb{Target: target, Version: version, Args: []string{"workspace", "converge"}}
	local := func() error {
		return errors.New("the workspace stage runs on the box, and setup always names one")
	}
	if err := relay.Run(ctx, exec, verb, local, stdout, stderr); err != nil {
		return fmt.Errorf("the workspace did not converge: %w", err)
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
//
// The admin machine's own tailscale and ssh commands run through the same exec
// the command surface was handed, rather than one built here: it is the local
// process boundary the whole command already reaches every binary over.
func prepareTailscale(ctx context.Context, conn *connection.SSH, exec connection.Exec, host, authKeyRef string, stdout io.Writer) (*tailscale.Access, func() (string, error), error) {
	// Fail fast before mutating anything when the key could never be obtained:
	// no reference to resolve and no interactive terminal to prompt. Acquisition
	// itself is deferred to enroll, so an already-satisfied re-run needs no key.
	term := secret.NewStdTerminal()
	if authKeyRef == "" && !term.Interactive() {
		return nil, nil, fmt.Errorf("resolve tailscale auth key: %w", secret.ErrNoReference)
	}

	admin := tailscale.NewAdmin(exec)
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
// open and reports which prerequisite to fix (exit 1, partial but reachable). A
// box already enrolled and reachable is reported as an already-satisfied no-op.
//
// It returns the establish result rather than telling the operator how to get
// back in: the address it proved is the one the box is registered under, so the
// line worth printing names the box, and only the registration knows its name.
func establishTailscale(ctx context.Context, access *tailscale.Access, host string, acquireKey func() (string, error), stdout io.Writer) (tailscale.Result, error) {
	result, err := access.Establish(ctx, tailscale.EstablishOptions{Host: host, AcquireKey: acquireKey})
	if err != nil {
		return tailscale.Result{}, fmt.Errorf("tailscale access not established: %w", err)
	}
	headline := "tailscale reach established over %s; public SSH closed.\n"
	if result.AlreadySatisfied {
		headline = "tailscale access already satisfied over %s; public SSH already closed.\n"
	}
	if _, err := fmt.Fprintf(stdout, headline, result.TailnetIP); err != nil {
		return tailscale.Result{}, fmt.Errorf("write success: %w", err)
	}
	return result, nil
}

// setupConclusion is what a finished setup knows about the box it provisioned:
// the access layer it ran under, the tailnet address tailscale mode
// established, the target the operator pointed smith at with --target, the
// address the run reached the box over, and every source of a name for the box.
type setupConclusion struct {
	accessMode string
	tailnetIP  string
	target     string
	addressed  string
	names      inventory.Naming
}

// concludeSetup proves the ongoing target and registers the box under its name.
// It is an admin-side step after the phases rather than a ninth phase: it runs
// once every phase has completed, touches nothing on the box, and so cannot
// revert or damage what the phases established.
//
// The invariant it keeps is that the inventory only ever holds a target smith
// proved. Public mode opens one connection as the smith user, the reach the
// box's own loopback check says nothing about. Tailscale mode opens none: the
// tailnet address is the one the lock-out-safety probe already came in over, so
// storing it is what keeps a wrong MagicDNS name out of the inventory.
//
// An operator's --target is the one target smith does not prove: they are
// pointing at their own naming scheme — an ssh_config alias, a MagicDNS name, a
// bastion hop — which smith cannot meaningfully verify and should not
// second-guess. It is stored verbatim and no probe is run.
//
// The name is resolved by precedence, and a name that differs from the one the
// box's marker already recorded is a rename: the entry moves rather than a
// second name for one machine appearing beside the first. The entry only moves
// when it is this box's — the marker records the name setup stamped on it, and
// a box whose first registration was refused for a name another machine holds
// carries that name too.
//
// A box that will not answer is provisioned but unregistered: the phases
// genuinely completed, so this reports the partial outcome and names the
// command that registers the box later rather than failing the setup.
func concludeSetup(ctx context.Context, exec connection.Exec, home config.Home, c setupConclusion, stdout, stderr io.Writer) error {
	name, _ := inventory.Name(c.names)
	target, err := provenTarget(ctx, exec, c)
	if err != nil {
		return err
	}
	if target == "" {
		unproved := smithTarget(c.names.Host)
		return reportUnregistered(stderr, unproved, c.names.Flag,
			fmt.Errorf("smith could not reach the box as %s", unproved))
	}
	moved, err := registerBox(home, registration{
		recorded:  c.names.Marker,
		addressed: c.addressed,
		name:      name,
		target:    target,
	})
	if err != nil {
		return reportUnnamed(stderr, target, err)
	}
	if _, err := fmt.Fprint(stdout, registeredReport(name, target, moved)); err != nil {
		return fmt.Errorf("write registration: %w", err)
	}
	return nil
}

// provenTarget returns the target the box is reached by from now on, having
// proved it in the one mode where it is not already proved. A --target is the
// operator's own and is returned untouched; a tailscale run returns the tailnet
// address the lock-out-safety probe came in over; a public run opens the
// connection that decides whether there is anything to register at all.
//
// A box that did not answer is ("", nil), the convention connection.Reachable
// already uses: an unreachable box is a fact about the box, not a failure of
// the probe, and the caller has a partial outcome to report rather than an
// error to raise.
func provenTarget(ctx context.Context, exec connection.Exec, c setupConclusion) (string, error) {
	if c.target != "" {
		return c.target, nil
	}
	if c.accessMode == "tailscale" {
		return smithTarget(c.tailnetIP), nil
	}
	target := smithTarget(c.names.Host)
	reachable, err := connection.Reachable(ctx, connection.New(target, exec))
	if err != nil {
		return "", fmt.Errorf("probe %s: %w", target, err)
	}
	if !reachable {
		return "", nil
	}
	return target, nil
}

// smithTarget is the ongoing ssh target for a box reachable at host.
func smithTarget(host string) string { return smithLogin + "@" + host }

// registeredReport renders what a successful setup's registration tells the
// operator: the rename, when the registration moved the box's entry off the
// name it was registered under before; the name the box answers to from now on; the target it
// resolves to; and the commands to type instead of an address — which is the
// whole point of writing the target down.
func registeredReport(name, target, moved string) string {
	var b strings.Builder
	if moved != "" {
		fmt.Fprintf(&b, "renamed %q to %q\n", moved, name)
	}
	fmt.Fprintf(&b, `registered %s as %q

Reach it by name from now on:
  smith machine status %s
  smith machine setup %s
`, target, name, name, name)
	return b.String()
}

// reportUnregistered reports a box the phases provisioned that smith did not
// register, and exits partial. The setup is not a failure — every phase
// completed and the box is provisioned and secured — so what the operator is
// owed is the state they are in and the command that closes the gap.
func reportUnregistered(stderr io.Writer, target, name string, cause error) error {
	if _, err := fmt.Fprint(stderr, unregisteredReport(target, name, cause)); err != nil {
		return fmt.Errorf("write registration failure: %w", err)
	}
	return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
}

// reportUnnamed reports a box the phases provisioned that smith could not
// write down — most often because the name it resolved to already reaches a
// different box — and exits partial. The box answered, so what is owed is a
// registration command with a name the operator picks, not the one that was
// just refused.
func reportUnnamed(stderr io.Writer, target string, cause error) error {
	if _, err := fmt.Fprint(stderr, unnamedReport(target, cause)); err != nil {
		return fmt.Errorf("write registration failure: %w", err)
	}
	return &exitError{code: bootstrap.OutcomePartial.ExitCode()}
}

// unnamedReport renders the provisioned-but-unnamed outcome: what smith refused
// to write down, what is nonetheless true of the box, and the command that
// registers it under a name that is free.
func unnamedReport(target string, cause error) string {
	return fmt.Sprintf(`the box is provisioned but unregistered: %v

Every setup phase completed, so the box is provisioned and secured; smith just
did not write it down. Register it under a name that is free:
  smith machine add %s --name <name>
`, cause, target)
}

// unregisteredReport renders the provisioned-but-unregistered outcome: what
// went wrong, what is nonetheless true of the box, and the registration command
// to run once it answers.
func unregisteredReport(target, name string, cause error) string {
	add := "smith machine add " + target
	if name != "" {
		add += " --name " + name
	}
	return fmt.Sprintf(`the box is provisioned but unregistered: %v

Every setup phase completed, so the box is provisioned and secured; smith just
has no name it can reach it by. Register it once it answers:
  %s
`, cause, add)
}
