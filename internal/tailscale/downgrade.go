package tailscale

import (
	"fmt"

	"github.com/byranZA/smith/internal/config"
)

// DowngradeError reports a run that would reopen public SSH on a box
// provisioned with tailscale access, without the operator saying so on the
// command line. It carries both modes and where the resolved one came from,
// because the operator has to see which link of the chain chose public.
type DowngradeError struct {
	// Box is the name of the box the run targets.
	Box string
	// Recorded is the access mode the box's marker records.
	Recorded string
	// Resolved is the access mode this run resolved, with its origin.
	Resolved config.Value
}

// Error implements error.
func (e *DowngradeError) Error() string {
	return fmt.Sprintf("%s was provisioned with access %s, but this run resolves access: %s; "+
		"pass --access tailscale to keep it, or --access public to reopen public SSH",
		e.Box, e.Recorded, e.Resolved)
}

// Downgrade applies the access-downgrade rule to the access mode a box's
// marker records and the access this run resolved: a run that would reopen
// public SSH on a box provisioned with tailscale access is refused, unless
// --access said so. It guards the resolved value rather than joining the
// precedence chain, so `blueprint check` and setup still report the same
// access. A blueprint's public does not get past it: one blueprint describes
// many boxes, and editing it is not a decision to reopen public SSH on every
// one of them, where the flag is a decision about this box.
//
// public to tailscale is not guarded, because lock-out safety already closes
// public SSH only once the tailnet proves reach, and nor is a box whose marker
// records no access mode, which has nothing to downgrade.
//
// It is a decision on values: it reaches no box, so a refusal leaves the box
// exactly as it was.
func Downgrade(box, recorded string, resolved config.Value) error {
	if recorded != "tailscale" || resolved.Value != "public" || resolved.Origin == config.FromFlag {
		return nil
	}
	return &DowngradeError{Box: box, Recorded: recorded, Resolved: resolved}
}
