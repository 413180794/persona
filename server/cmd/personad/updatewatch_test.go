package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/update"
)

// Without a service manager the watchdog stops the hub that would not come up and starts the restored binary itself,
// with the arguments the hub had (real processes: a sleeping stand-in for the hub, a script for the old binary).
func TestRestartHubWithoutAServiceManager(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("XPC_SERVICE_NAME", "")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(dir, "started")
	target := filepath.Join(dir, "bin", "jarvisd")
	script := "#!/bin/sh\necho \"$@\" > " + started + "\n"
	if err := os.WriteFile(target, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	hub := exec.Command("sleep", "60")
	if err := hub.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- hub.Wait() }()
	if err := restartHub(update.Pending{PID: hub.Process.Pid, Target: target, Args: []string{"/old/path/jarvisd", "hub", "run"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		hub.Process.Signal(syscall.SIGKILL)
		t.Fatal("the hub that would not come up was not stopped")
	}
	// Only a guard against a hang: on macOS the first run of a freshly written script waits for the system's checks,
	// up to 10 s while go test ./... links its test binaries (#2307).
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(started); err == nil {
			if string(b) != "hub run\n" {
				t.Fatalf("started with %q", b)
			}
			return
		}
	}
	t.Fatal("the restored binary was not started")
}
