package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantStderrWaitHelper(t *testing.T) {
	switch os.Getenv("PERSONA_TEST_STDERR_WAIT_HELPER") {
	case "1":
		fmt.Fprint(os.Stderr, "launcher setup failed\n\x1b[31mERROR\x1b[0m: last startup diagnostic")
		os.Exit(1)
	case "long":
		fmt.Fprintln(os.Stderr, strings.Repeat("x", 128*1024))
		os.Exit(0)
	}
}

func TestAssistantStderrSurvivesWait(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAssistantStderrWaitHelper$")
	home := t.TempDir()
	cmd.Env = []string{"PERSONA_TEST_STDERR_WAIT_HELPER=1", "GOMAXPROCS=1", "HOME=" + home, "TMPDIR=" + home}
	stderr, closeWriter, err := assistantStderrPipe(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	startErr := cmd.Start()
	closeWriter()
	if startErr != nil {
		t.Fatal(startErr)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("helper exit: %v", err)
	}
	p := &assistantProc{id: "stderr-test-session", launcher: "test-launcher", cmd: cmd, born: time.Now(),
		exited: make(chan struct{}), stderrDone: make(chan struct{})}
	close(p.exited)
	go watchAssistantStderr(log.New(io.Discard, "", 0), p, stderr)
	select {
	case <-p.stderrDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stderr reader did not finish after the process exited")
	}
	msg := p.startFailure().Error()
	if !strings.Contains(msg, "launcher setup failed | ERROR: last startup diagnostic") || strings.Contains(msg, "\x1b") {
		t.Fatalf("startup diagnostic was lost after Wait: %q", msg)
	}
	if _, err := stderr.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("stderr reader did not close its pipe: %v", err)
	}
}

func TestAssistantStderrLongLineDoesNotBlockWait(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestAssistantStderrWaitHelper$")
	home := t.TempDir()
	cmd.Env = []string{"PERSONA_TEST_STDERR_WAIT_HELPER=long", "GOMAXPROCS=1", "HOME=" + home, "TMPDIR=" + home}
	stderr, closeWriter, err := assistantStderrPipe(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	startErr := cmd.Start()
	closeWriter()
	if startErr != nil {
		t.Fatal(startErr)
	}
	p := &assistantProc{id: "stderr-test-session", launcher: "test-launcher", born: time.Now(), stderrDone: make(chan struct{})}
	go watchAssistantStderr(log.New(io.Discard, "", 0), p, stderr)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("long stderr line blocked or broke the process: %v", err)
	}
	select {
	case <-p.stderrDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stderr reader did not drain the long line")
	}
}

func TestAssistantStderrStartFailure(t *testing.T) {
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing-executable"))
	stderr, closeWriter, err := assistantStderrPipe(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	startErr := cmd.Start()
	closeWriter()
	if startErr == nil {
		t.Fatal("missing executable started")
	}
	if data, err := io.ReadAll(stderr); err != nil || len(data) != 0 {
		t.Fatalf("parent pipe after failed Start: bytes=%d err=%v", len(data), err)
	}
}
