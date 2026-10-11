//go:build !unix

package cli

import "errors"

// switchProcess reports that this platform cannot replace a running process;
// update refuses such a platform before it reaches the switch.
func switchProcess(string, []string) error {
	return errors.New("replacing the running process is not supported on this platform")
}
