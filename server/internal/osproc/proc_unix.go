//go:build !windows

package osproc

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// GroupAttr starts a child as the leader of its own process group, so StopGroup reaches its children too.
func GroupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// DetachedAttr starts a child in its own session, so it outlives this process.
func DetachedAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// StopGroup signals the process group led by pid: SIGTERM, or SIGKILL when force.
func StopGroup(pid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	return syscall.Kill(-pid, sig)
}

// Kill ends the process pid at once (SIGKILL); one already gone is not an error.
func Kill(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// TerminateSelf ends this process the way a service manager's stop would (SIGTERM: a clean shutdown).
func TerminateSelf() { _ = syscall.Kill(os.Getpid(), syscall.SIGTERM) }

// Alive says whether a process with this pid exists (one owned by another user counts).
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// OwnedByMe says whether the file belongs to the current user.
func OwnedByMe(st os.FileInfo) bool {
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && sys.Uid == uint32(os.Getuid())
}

// CPUTime is the user plus system CPU time this process has used.
func CPUTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
