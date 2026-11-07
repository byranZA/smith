//go:build windows

package stack

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive lock on f without waiting, and reports whether
// it got it. The system frees the lock when f closes or its process dies.
func tryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("LockFileEx: %w", err)
	}
	return true, nil
}

// unlock frees f's lock.
func unlock(f *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return fmt.Errorf("UnlockFileEx: %w", err)
	}
	return nil
}
