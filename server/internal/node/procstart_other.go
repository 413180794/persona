//go:build !linux

package node

// procStartMark: only Linux (/proc) tells when a process started; elsewhere a pid is trusted while it is alive.
func procStartMark(int) string { return "" }

// claudeFileStart: see procstart_linux.go; nothing to compare with here.
func claudeFileStart(string) string { return "" }
