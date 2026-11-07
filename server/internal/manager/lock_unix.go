//go:build unix

package manager

import (
	"errors"
	"os"
	"syscall"
)

// lockFile holds an exclusive lock on path for the life of this process (the descriptor is kept open; an exec closes
// it, and the program started in its place takes it again): errLocked when another process holds it.
func lockFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errLocked
		}
		return err
	}
	held = append(held, f)
	return nil
}
