//go:build !windows

package stack

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// tryLock takes an exclusive advisory lock on f without waiting, and reports
// whether it got it. The kernel frees the lock when f closes or its process
// dies.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("flock: %w", err)
	}
	return true, nil
}

// unlock frees f's lock.
func unlock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("unlock: %w", err)
	}
	return nil
}
