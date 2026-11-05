//go:build linux

package node

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
