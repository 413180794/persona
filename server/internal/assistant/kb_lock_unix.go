//go:build !windows

package assistant

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockKBDir takes the data directory for this process (.lock in it, not waiting): one assistant process writes the
// knowledge base at a time. The lock goes with the process.
func lockKBDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another assistant process has the data directory")
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
