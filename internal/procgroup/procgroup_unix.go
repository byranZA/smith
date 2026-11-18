//go:build !windows

// Package procgroup makes a command's cancellation take down everything it started.
package procgroup

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Stop starts cmd in a process group of its own and has its cancellation kill
// the whole group, so a cancelled command takes the processes it started down
// with it. Callers set cmd.WaitDelay themselves.
func Stop(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill process group %d: %w", cmd.Process.Pid, err)
		}
		return nil
	}
}
