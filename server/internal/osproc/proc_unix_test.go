//go:build !windows

package osproc

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Alive counts another user's process (init), AliveMine does not: a pid this user wrote down that init now has is a
// reused one (#2141). Both see this process, neither a pid that does not exist.
func TestAliveMineIsThisUsersProcesses(t *testing.T) {
	if !AliveMine(os.Getpid()) || !Alive(os.Getpid()) {
		t.Fatal("this process is not alive")
	}
	if AliveMine(0) || AliveMine(-1) {
		t.Fatal("pid 0 / -1 (the process group, every process) counted")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if AliveMine(cmd.Process.Pid) {
		t.Fatalf("a reaped child %d counted", cmd.Process.Pid)
	}
	if os.Geteuid() == 0 {
		t.Skip("root may signal every process: no other user's process to tell apart")
	}
	if !Alive(1) {
		t.Skip("init is not visible here (a PID namespace without it)")
	}
	if AliveMine(1) {
		t.Fatal("init (another user's process) counted as this user's")
	}
}

func TestStopGroupTakesTheChildren(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $!; wait")
	cmd.SysProcAttr = GroupAttr()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var child int
	if _, err := fmt.Fscan(out, &child); err != nil {
		t.Fatal(err)
	}
	if !Alive(child) {
		t.Fatal("child not running")
	}
	if err := StopGroup(cmd.Process.Pid, true); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for Alive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if Alive(child) {
		t.Fatal("StopGroup left the shell's child running")
	}
}

// SessionGroupAttr puts the child in a session (and so a process group) of its own, with no controlling terminal,
// and StopGroup still ends it (#3265).
func TestSessionGroupAttrHasItsOwnSession(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = SessionGroupAttr()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// golang.org/x/sys/unix: Linux's syscall package has no Getsid.
	if sid, err := unix.Getsid(pid); err != nil || sid != pid {
		t.Fatalf("session %d (%v), want the child's own %d", sid, err, pid)
	}
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Fatalf("process group %d (%v), want the child's own %d", pgid, err, pid)
	}
	if err := StopGroup(pid, true); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("StopGroup did not end it")
	}
}

// A group that keeps forking can keep a child through one SIGKILL to the group on macOS (one forked while it is
// delivered); SweepGroup leaves it empty (#5942).
func TestSweepGroupLeavesNothing(t *testing.T) {
	for round := 0; round < 10; round++ {
		cmd := exec.Command("/bin/sh", "-c", `while :; do sleep 30 & sleep 0.01; done`)
		cmd.SysProcAttr = GroupAttr()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { _ = cmd.Wait() }()
		pid := cmd.Process.Pid
		time.Sleep(50 * time.Millisecond)
		_ = StopGroup(pid, true)
		if !SweepGroup(pid, 2*time.Second) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			t.Fatalf("round %d: the group still has processes after the sweep", round)
		}
		if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
			t.Fatalf("round %d: kill(-group, 0) = %v, want ESRCH", round, err)
		}
	}
}
