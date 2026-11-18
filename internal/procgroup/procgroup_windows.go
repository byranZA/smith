//go:build windows

// Package procgroup makes a command's cancellation take down everything it started.
package procgroup

import (
	"fmt"
	"os/exec"
	"strconv"
)

// Stop has cmd's cancellation kill the command and every process descended
// from it, so a cancelled command takes the processes it started down with it.
// Callers set cmd.WaitDelay themselves.
func Stop(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		pid := strconv.Itoa(cmd.Process.Pid)
		if out, err := exec.Command("taskkill", "/T", "/F", "/PID", pid).CombinedOutput(); err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("kill process tree %s: %w: %s", pid, err, out)
		}
		return nil
	}
}
