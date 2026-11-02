//go:build windows

package link

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f, waiting for it unless wait is false; errBusy when it is held and wait is false.
// The lock goes away with the process, however it ends.
func lockFile(f *os.File, wait bool) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errBusy
	}
	return err
}
