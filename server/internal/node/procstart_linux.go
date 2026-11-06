//go:build linux

package node

import (
	"errors"
	"io/fs"
	"syscall"
)

// procStartMark is when pid started as "linux:<starttime>" (clock ticks since boot, field 22 of /proc/<pid>/stat), the
// same number Claude Code writes as procStart in its sessions/<pid>.json. A reused pid has another one. "" when the
// process is gone.
func procStartMark(pid int) string {
	f, err := procStat(pid)
	if err != nil || len(f) < 20 {
		return ""
	}
	return "linux:" + f[19] // field 22
}

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
	f, err := procStat(pid)
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
