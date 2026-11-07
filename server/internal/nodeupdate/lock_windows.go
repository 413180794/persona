package nodeupdate

import (
	"github.com/nwverse/persona/server/internal/compat"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockDir locks a file inside the directory: Windows cannot lock a directory handle. The file stays (removing it
// would let a third updater lock a new file while a second holds the old one).
func lockDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, compat.NodeUpdateLock), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
		f.Close()
	}, nil
}
