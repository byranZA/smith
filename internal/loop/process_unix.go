//go:build !windows

package loop

import (
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
