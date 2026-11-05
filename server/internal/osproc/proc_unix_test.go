//go:build !windows

package osproc

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
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
