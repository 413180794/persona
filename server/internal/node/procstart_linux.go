//go:build linux

package node

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/nwverse/persona/server/internal/procinfo"
)

// claudeFileStart is the start a sessions/<pid>.json's procStart stands for on this platform.
func claudeFileStart(procStart string) string {
	if procStart == "" {
		return ""
	}
	return "linux:" + procStart
}

// procGone says pid is surely no longer the process seen with start: no /proc entry (or ESRCH while it goes), a zombie
// (killed, not reaped yet: its stat still reads with the old start), or another start (a reused pid). Any other read
// error (EMFILE, ENOMEM) says nothing: not gone.
func procGone(pid int, start string) bool {
	f, err := procinfo.Stat(pid)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
	}
	if len(f) < 20 {
		return false
	}
	if f[0] == "Z" || f[0] == "X" {
		return true
	}
	return start != "" && "linux:"+f[19] != start
}
