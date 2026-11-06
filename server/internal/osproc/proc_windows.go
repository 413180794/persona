package osproc

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
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

// SessionGroupAttr is GroupAttr: Windows has no controlling terminal to keep the child from (the Unix one, #3265).
func SessionGroupAttr() *syscall.SysProcAttr { return GroupAttr() }

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

// SweepGroup is StopGroup's follow-up on Unix (proc_unix.go); taskkill /T has already walked the tree, so there is
// no group to sweep here.
func SweepGroup(int, time.Duration) bool { return true }

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

// AliveMine says whether pid is a running process this user may open (any process, for an administrator): the check
// for a pid this user's own program wrote down. One this user may not open (another user's, a protected one) is not
// the process that wrote it: that one has exited and the pid was reused (#2141).
func AliveMine(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
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

// Exec runs path as this process's stand-in: Windows has no exec, so it starts path as a child on the same console
// (stdin, stdout and stderr inherited: the terminal pane sees the program, not this process), and exits with the
// child's exit code once it ends. Ctrl+C and Ctrl+Break reach the child from the console by themselves (same console,
// same process group), so this process takes them and drops them instead of dying first and leaving the child without
// a parent: on Windows only Notify turns off their default exit (os/signal's docs), Ignore does not promise that.
// It returns only when the child could not start.
func Exec(path string, argv, env []string) error {
	cmd := exec.Command(path)
	cmd.Args = argv
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	go func() {
		for range interrupts {
		}
	}()
	if err := cmd.Start(); err != nil {
		signal.Stop(interrupts)
		close(interrupts)
		return err
	}
	err := cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &exit):
		os.Exit(exit.ExitCode())
	}
	os.Exit(1)
	return nil
}
