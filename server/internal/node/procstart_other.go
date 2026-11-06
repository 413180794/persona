//go:build !linux

package node

// procStartMark: only Linux (/proc) tells when a process started; elsewhere a pid is trusted while it is alive.
func procStartMark(int) string { return "" }

// claudeFileStart: see procstart_linux.go; nothing to compare with here.
func claudeFileStart(string) string { return "" }

// procGone: see procstart_linux.go; here only whether the pid is alive (a zombie still counts as alive).
func procGone(pid int, _ string) bool { return !alive(pid) }
