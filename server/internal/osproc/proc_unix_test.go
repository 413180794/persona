//go:build !windows

package osproc

import (
	"fmt"
	"os/exec"
	"testing"
	"time"
)

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
