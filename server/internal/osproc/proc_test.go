package osproc

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestAliveAndCPUTime(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Fatal("this process is not alive")
	}
	before := CPUTime()
	for i, x := 0, 0; i < 20_000_000; i++ {
		x += i
	}
	if after := CPUTime(); after < before {
		t.Fatalf("CPU time went back: %v then %v", before, after)
	}
}

// TestKillHelper is the child TestKill ends: it waits until killed.
func TestKillHelper(t *testing.T) {
	if os.Getenv("OSPROC_KILL_HELPER") == "" {
		t.Skip("helper process only")
	}
	time.Sleep(time.Minute)
}

// Kill ends a process at once; one already gone is no error.
func TestKill(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestKillHelper$")
	cmd.Env = append(os.Environ(), "OSPROC_KILL_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if err := Kill(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("still running after Kill")
	}
	if err := Kill(cmd.Process.Pid); err != nil {
		t.Fatalf("Kill of a process already gone: %v", err)
	}
}
