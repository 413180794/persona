package osproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// GroupAttr starts a child in a new process group; StopGroup takes its tree with it.
func GroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// DetachedAttr starts a child without this process's console, so it outlives this process.
func DetachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}

// StopGroup ends the process tree rooted at pid with taskkill /F, force or not: without /F taskkill only posts
// WM_CLOSE, which console programs (claude, node, plugins) never get, so the polite stop would just wait out its
// timeout. A tree whose root already exited loses its orphans; a Job Object would hold them (#1090).
func StopGroup(pid int, _ bool) error {
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	return exec.Command(taskkill, "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}

// Kill ends the process pid at once (TerminateProcess); one already gone is not an error.
func Kill(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER { // no process with that pid
			return nil
		}
		return err
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil && Alive(pid) {
		return err
	}
	return nil
}

// TerminateSelf exits with status 1: Windows has no SIGTERM to send to oneself, so the clean shutdown (deferred
// cleanup, writes in flight) is skipped, and only a failure status makes a service manager start it again.
func TerminateSelf() { os.Exit(1) }

// Alive says whether a process with this pid is still running (one this user may not open counts).
func Alive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}

// OwnedByMe is false on Windows: FileInfo carries no owner, so checks that need one refuse.
func OwnedByMe(os.FileInfo) bool { return false }

// CPUTime is the user plus kernel CPU time this process has used.
func CPUTime() time.Duration {
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user) != nil {
		return 0
	}
	ticks := func(t windows.Filetime) int64 { return int64(t.HighDateTime)<<32 | int64(t.LowDateTime) }
	return time.Duration(ticks(kernel)+ticks(user)) * 100 // Filetime counts 100 ns ticks
}
