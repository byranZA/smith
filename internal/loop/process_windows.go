//go:build windows

package loop

// killed reports false: Windows has no SIGKILL for an agent to exit by.
func killed(error) bool { return false }
