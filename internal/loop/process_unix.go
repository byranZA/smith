//go:build !windows

package loop

import (
	"errors"
	"os/exec"
	"syscall"
)

// sigkillStatus is the exit status a shell gives for a child killed by SIGKILL.
const sigkillStatus = 128 + int(syscall.SIGKILL)

// killed reports whether err is a process's exit by SIGKILL, or with the
// status a shell exits with for a child so killed.
func killed(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	return status.Signaled() && status.Signal() == syscall.SIGKILL || status.Exited() && status.ExitStatus() == sigkillStatus
}
