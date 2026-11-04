package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
	"github.com/nwverse/persona/server/internal/update"
)

// runUpdateWatch is `jarvisd update-watch <pending.json>`: the old binary's watchdog over an update (hub/selfupdate.go,
// docs/hub-update.md「回滚」). The hub starts it just before running the new version; if that does not answer as the
// new build with the update confirmed within 3 minutes, the old binary goes back and the hub restarts on it.
func runUpdateWatch(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: jarvisd update-watch <pending.json>")
	}
	var p update.Pending
	ok, err := update.Load(args[0], &p)
	if err != nil || !ok {
		return err // nothing under way
	}
	return update.Watch(ctx, &http.Client{Timeout: 5 * time.Second}, p, args[0], time.Now().Add(3*time.Minute), 3*time.Second,
		func() error { return update.RestoreAll(p) }, // the hub and the programs swapped with it
		func() error { return restartHub(p) })
}

// restartHub stops the hub that would not come up; a service manager (systemd, launchd) starts it again on the
// binary just restored, otherwise this starts it with the arguments it had.
func restartHub(p update.Pending) error {
	if proc, err := os.FindProcess(p.PID); err == nil && p.PID > 1 {
		_ = proc.Signal(syscall.SIGTERM)
		for i := 0; i < 20 && proc.Signal(syscall.Signal(0)) == nil; i++ {
			time.Sleep(500 * time.Millisecond)
		}
		_ = proc.Signal(syscall.SIGKILL)
	}
	if supervised() {
		return nil
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(p.Target), "..", "update", "hub.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log = nil
	}
	args := []string{"hub"}
	if len(p.Args) > 1 {
		args = p.Args[1:]
	}
	cmd := exec.Command(p.Target, args...)
	if log != nil {
		cmd.Stdout, cmd.Stderr = log, log
	}
	cmd.SysProcAttr = osproc.DetachedAttr()
	return cmd.Start()
}

// supervised: the hub runs under systemd (INVOCATION_ID) or launchd (XPC_SERVICE_NAME, "0" in a Terminal shell).
func supervised() bool {
	x := os.Getenv("XPC_SERVICE_NAME")
	return os.Getenv("INVOCATION_ID") != "" || (x != "" && x != "0")
}
