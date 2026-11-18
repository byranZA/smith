//go:build !windows

package connection

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// groupStopDelay is how long a cancelled command's output pipes may stay open after its group is killed.
const groupStopDelay = 2 * time.Second

// stopTogether starts cmd in a process group of its own and has its
// cancellation kill the whole group, so a cancelled command takes the
// processes it started down with it.
func stopTogether(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill process group %d: %w", cmd.Process.Pid, err)
		}
		return nil
	}
	cmd.WaitDelay = groupStopDelay
}
