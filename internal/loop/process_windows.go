//go:build windows

package loop

import "os/exec"

// stopTogether leaves proc's cancellation as the default kill of the agent
// alone; Launch's wait delay keeps a lingering child from holding smith up.
func stopTogether(*exec.Cmd) {}
