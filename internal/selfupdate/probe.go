package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// rejectedExitCode is the status smith exits with when cobra rejects an unknown
// command or flag, which is how a release older than `smith update` answers.
const rejectedExitCode = 1

// ContinueFlag is the hidden flag a freshly installed smith is started with
// after an update replaced the old one: it runs the box step alone, on the new
// code, and never repeats the local update.
const ContinueFlag = "continue-boxes"

// Probe reports whether the smith binary at path can continue the box step, by
// asking it for the help of the update command run with ContinueFlag. Only a
// binary that runs and rejects the command is reported old; any other failure
// is an error.
func Probe(ctx context.Context, path string) (bool, error) {
	err := exec.CommandContext(ctx, path, "update", "--"+ContinueFlag, "--help").Run() // #nosec G204 -- path is the smith binary update just installed.
	if err == nil {
		return true, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, fmt.Errorf("probe %s: %w", path, ctxErr)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() && exit.ExitCode() == rejectedExitCode {
		return false, nil
	}
	return false, fmt.Errorf("probe %s: %w", path, err)
}

// ProbeError reports a new binary installed at Path whose support for the box
// step could not be established, so the boxes were left alone.
type ProbeError struct {
	// Path is where the new binary is installed.
	Path string
	// Target is the version it carries.
	Target string
	// Err is why the probe failed.
	Err error
}

// Error implements error.
func (e *ProbeError) Error() string {
	return fmt.Sprintf("smith %s is installed at %s, but checking whether it can upgrade the boxes failed, so no box was touched: %v", e.Target, e.Path, e.Err)
}

// Unwrap returns the probe failure.
func (e *ProbeError) Unwrap() error { return e.Err }
