//go:build !windows

package loop

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// stopTogether starts proc in a process group of its own and has its
// cancellation kill the whole group, so an interrupted agent takes the
// processes it started down with it.
func stopTogether(proc *exec.Cmd) {
	proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	proc.Cancel = func() error {
		if err := syscall.Kill(-proc.Process.Pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill process group %d: %w", proc.Process.Pid, err)
		}
		return nil
	}
}

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
