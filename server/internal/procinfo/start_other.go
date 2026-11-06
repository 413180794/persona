//go:build !linux

package procinfo

// StartMark: only Linux (/proc) tells when a process started; elsewhere a pid is trusted while it is alive.
func StartMark(int) string { return "" }
