package selfupdate

import (
	"context"
	"os/exec"
)

// ContinueFlag is the hidden flag a freshly installed smith is started with
// after an update replaced the old one: it runs the box step alone, on the new
// code, and never repeats the local update.
const ContinueFlag = "continue-boxes"

// Probe reports whether the smith binary at path can continue the box step, by
// asking it for the help of the update command run with ContinueFlag.
func Probe(ctx context.Context, path string) (bool, error) {
	probe := exec.CommandContext(ctx, path, "update", "--"+ContinueFlag, "--help") // #nosec G204 -- path is the smith binary update just installed.
	return probe.Run() == nil, nil
}
