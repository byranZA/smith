package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/byranZA/smith/internal/bootstrap"
	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/status"
)

// smithLogin is the login a box smith has provisioned is reached as from now
// on. The bootstrap-in login is dead by the time setup finishes — hardening
// closes root logins — so the ongoing target is the smith user's, and it is
// the one smith proves before writing anything down.
const smithLogin = "smith"

// setupConclusion is what a finished setup knows about the box it provisioned:
// the access layer it ran under, the host the operator addressed the box by,
// the tailnet address tailscale mode established, and the name the operator
// chose — empty when they chose none.
type setupConclusion struct {
	accessMode string
	host       string
	tailnetIP  string
	name       string
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
// A box that will not answer is provisioned but unregistered: the phases
// genuinely completed, so this reports the partial outcome and names the
// command that registers the box later rather than failing the setup.
func concludeSetup(ctx context.Context, exec connection.Exec, home config.Home, c setupConclusion, stdout, stderr io.Writer) error {
	name := c.name
	if name == "" {
		name = c.host
	}
	target := smithTarget(c.tailnetIP)
	if c.accessMode != "tailscale" {
		target = smithTarget(c.host)
		reachable, err := status.Reachable(ctx, connection.New(target, exec))
		if err != nil {
			return fmt.Errorf("probe %s: %w", target, err)
		}
		if !reachable {
			return reportUnregistered(stderr, target, c.name,
				fmt.Errorf("smith could not reach the box as %s", target))
		}
	}
	if err := registerBox(home, name, target); err != nil {
		return reportUnregistered(stderr, target, c.name, err)
	}
	if _, err := fmt.Fprint(stdout, registeredReport(name, target)); err != nil {
		return fmt.Errorf("write registration: %w", err)
	}
	return nil
}

// smithTarget is the ongoing ssh target for a box reachable at host.
func smithTarget(host string) string { return smithLogin + "@" + host }

// registeredReport renders what a successful setup's registration tells the
// operator: the name the box answers to from now on, the target it resolves to,
// and the commands to type instead of an address — which is the whole point of
// writing the target down.
func registeredReport(name, target string) string {
	return fmt.Sprintf(`registered %s as %q

Reach it by name from now on:
  smith machine status %s
  smith machine setup %s
`, target, name, name, name)
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
