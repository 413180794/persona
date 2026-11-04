//go:build !windows

package transcript

import (
	"os"
	"syscall"
)

// inode is the file's inode number (0 where the platform does not say).
func inode(st os.FileInfo) uint64 {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}
