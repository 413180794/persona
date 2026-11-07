//go:build linux || darwin

package assistant

import "syscall"

// freeBytes is the space free to an unprivileged user on the file system that holds path.
func freeBytes(path string) (int64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
