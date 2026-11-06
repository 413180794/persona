//go:build !windows

package node

import (
	"os"
	"syscall"
)

// openNoBlock opens path for reading without waiting on it or following a link: a FIFO, or a link put where a
// file was looked at a moment before, neither hangs nor leads elsewhere.
func openNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
}
