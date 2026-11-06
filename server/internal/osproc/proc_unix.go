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

// SessionGroupAttr is GroupAttr in a session of its own: the child has no controlling terminal either, so an
// interactive shell (zsh -ic) started from a process that has one is not stopped by SIGTTOU / SIGTTIN for reaching
// for a terminal whose foreground it is not (#3265). The session's process group is the child's, so StopGroup still
// reaches its children.
func SessionGroupAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

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

// sweepEvery is how often SweepGroup kills the group again.
const sweepEvery = 10 * time.Millisecond

// SweepGroup kills the process group led by pid (SIGKILL) again and again until nobody is left in it or within has
// passed, and says whether it is empty. One signal to a group misses a child forked while it is delivered: macOS does
// not order the two (Linux does), and the child lives on, adopted by launchd, holding what the group held (#5942).
// Killed members count until their parent reaps them, which is quick: the caller's Wait, or launchd for orphans.
func SweepGroup(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if err := syscall.Kill(-pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(sweepEvery)
	}
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

// AliveMine says whether pid is a running process of this user (or any process, when this runs as root): the check
// for a pid this user's own program wrote down. A pid that now belongs to another user (kill gives EPERM) is not the
// process that wrote it: that one has exited and the pid was reused (#2141).
func AliveMine(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
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

// Exec replaces this process with path (argv[0] included in argv), the way a launcher hands its terminal to the
// program it starts. It returns only when the exec failed.
func Exec(path string, argv, env []string) error { return syscall.Exec(path, argv, env) }
