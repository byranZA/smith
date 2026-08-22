package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/inventory"
	"github.com/byranZA/smith/internal/status"
)

// smithLogin is the login a box smith has provisioned is reached as from now
// on. The bootstrap-in login is dead by the time setup finishes — hardening
// closes root logins — so the ongoing target is the smith user's, and it is
// the one smith proves before writing anything down.
const smithLogin = "smith"

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
// A box that did not answer is ("", nil), the convention status.Reachable
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
	reachable, err := status.Reachable(ctx, connection.New(target, exec))
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
