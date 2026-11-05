//go:build unix

package hub

import "syscall"

func closeOnExec(fd int) { syscall.CloseOnExec(fd) }

// keepOnExec clears close-on-exec: the fd goes on to the binary exec'd next.
func keepOnExec(fd int) {
	_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0)
}
