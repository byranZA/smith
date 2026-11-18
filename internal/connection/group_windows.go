//go:build windows

package connection

import "os/exec"

// stopTogether leaves cmd's cancellation as the default kill of the command itself.
func stopTogether(*exec.Cmd) {}
