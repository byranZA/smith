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

// Downgrade returns a *DowngradeError when a box whose marker records tailscale
// access resolves to public from anywhere but --access, and nil otherwise.
func Downgrade(box, recorded string, resolved config.Value) error {
	if recorded != "tailscale" || resolved.Value != "public" || resolved.Origin == config.FromFlag {
		return nil
	}
	return &DowngradeError{Box: box, Recorded: recorded, Resolved: resolved}
}
