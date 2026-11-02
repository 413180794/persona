//go:build unix

package link

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive lock on f, waiting for it unless wait is false; errBusy when it is held and wait is false.
// The lock goes away with the process, however it ends.
func lockFile(f *os.File, wait bool) error {
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	for {
		err := syscall.Flock(int(f.Fd()), how)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EWOULDBLOCK):
			return errBusy
		}
		return err
	}
}
