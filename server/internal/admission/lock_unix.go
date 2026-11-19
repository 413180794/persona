//go:build linux || darwin

package admission

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func checkDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return errors.New("admission directory must be a private directory owned by the service user")
	}
	return nil
}

func lockDir(dir string) (handle, error) {
	fd, err := syscall.Open(filepath.Join(dir, ".lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return handle{}, err
	}
	f := os.NewFile(uintptr(fd), "admission lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		for errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
			// Holder operations consist only of bounded local filesystem work.
			err = syscall.Flock(fd, syscall.LOCK_EX)
		}
		if err != nil {
			_ = f.Close()
			return handle{}, err
		}
	}
	return handle{f: f}, nil
}
