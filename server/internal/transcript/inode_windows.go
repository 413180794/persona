package transcript

import "os"

// inode is 0 on Windows: FileInfo carries no file index, so a replaced transcript is not told apart by it.
func inode(os.FileInfo) uint64 { return 0 }
