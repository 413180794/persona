//go:build linux

package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// A sealed kernel object pins the exact artifact through namespace mounting.
// Its descriptor is passed only to the trusted launcher, consumed during
// namespace mounting, and closed by the host's per-run disposal.
func newSourceSnapshot() (*os.File, error) {
	fd, err := unix.MemfdCreate("persona-source-artifact", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd)), nil
}

func sealSourceSnapshot(f *os.File) error {
	_, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL)
	return err
}

// Open by directory descriptors so a component replacement cannot introduce a
// symlink after SourceFile's cheap path checks. O_NONBLOCK prevents a raced-in
// FIFO/device from blocking the manager before the final regular-file check.
func openSourceFile(root, rel string) (*os.File, error) {
	if !sourcePath(rel) {
		return nil, errors.New("invalid source path")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), "source-artifact")
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("source artifact must be regular")
	}
	return f, nil
}
