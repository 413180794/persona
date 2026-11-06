//go:build darwin

package node

import (
	"os"
	"syscall"
)

// sfDataless is SF_DATALESS (sys/stat.h): only a placeholder of the file is here, and reading it fetches it.
const sfDataless = 0x40000000

// dataless says whether st (an Lstat's) is such a placeholder (iCloud's Desktop & Documents, a File Provider's).
func dataless(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Flags&sfDataless != 0
}
