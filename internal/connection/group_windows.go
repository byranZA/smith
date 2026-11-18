//go:build windows

package connection

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// groupStopDelay is how long a cancelled command's output pipes may stay open after its process tree is killed.
const groupStopDelay = 2 * time.Second

// stopTogether has cmd's cancellation kill the command and every process
// descended from it, so a cancelled command takes the processes it started
// down with it.
func stopTogether(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		pid := strconv.Itoa(cmd.Process.Pid)
		if out, err := exec.Command("taskkill", "/T", "/F", "/PID", pid).CombinedOutput(); err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("kill process tree %s: %w: %s", pid, err, out)
		}
		return nil
	}
	cmd.WaitDelay = groupStopDelay
}
