//go:build unix

package cli

import (
	"fmt"
	"os"
	"syscall"
)

// switchProcess replaces this process with the binary at path, run with args
// and this process's environment. It returns only when the switch failed.
func switchProcess(path string, args []string) error {
	if err := syscall.Exec(path, args, os.Environ()); err != nil { // #nosec G204 -- path is the smith binary update just installed.
		return fmt.Errorf("exec %s: %w", path, err)
	}
	return nil
}
