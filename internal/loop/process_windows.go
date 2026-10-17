//go:build windows

package loop

import "os/exec"

// stopTogether leaves proc's cancellation as the default kill of the agent
// alone; Launch's wait delay keeps a lingering child from holding smith up.
func stopTogether(*exec.Cmd) {}

// killed reports false: Windows has no SIGKILL for an agent to exit by.
func killed(error) bool { return false }
