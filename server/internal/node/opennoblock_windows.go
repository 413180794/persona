//go:build windows

package node

import "os"

// openNoBlock opens path for reading (Windows has no FIFOs in the file system to wait on).
func openNoBlock(path string) (*os.File, error) { return os.Open(path) }
