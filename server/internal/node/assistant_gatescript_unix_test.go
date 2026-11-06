//go:build !windows

package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// writeGateScript writes body as a bash script in a fresh directory and returns its path.
func writeGateScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gate.sh")
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func resultText(r proto.ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// runGateScriptWithin runs the script and fails the test when it has not returned within limit (the result is then
// left to the goroutine; the cleanup kills what the script started).
func runGateScriptWithin(t *testing.T, ctx context.Context, script string, limit time.Duration) (proto.ToolResult, time.Duration) {
	t.Helper()
	done := make(chan proto.ToolResult, 1)
	start := time.Now()
	go func() { done <- runGateScript(ctx, "gate", script, nil, filepath.Dir(script), nil) }()
	select {
	case res := <-done:
		return res, time.Since(start)
	case <-time.After(limit):
		t.Fatalf("runGateScript still running after %v: a child holding the output pipe keeps the tool call", limit)
		return proto.ToolResult{}, 0
	}
}

// killPidFileAtCleanup kills the pid the script writes to pidFile when the test ends, even one that failed before
// reading it.
func killPidFileAtCleanup(t *testing.T, pidFile string) {
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// leftoverPid reads the pid the script wrote down.
func leftoverPid(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(b)) != "" {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("the script did not write the background pid")
	}
	return pid
}

// pidGone says whether pid has exited within a short grace (the kill is asynchronous; a zombie of a reparented child is
// reaped by init soon after).
func pidGone(pid int) bool {
	for i := 0; i < 50; i++ {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// A script that leaves a background child holding its stdout and exits: the call returns soon after, not when the
// child does, and the child does not outlive the call.
func TestGateScriptBackgroundChildExitsWithTheScript(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := writeGateScript(t, "sleep 300 &\necho $! > "+pidFile+"\necho started\n")
	killPidFileAtCleanup(t, pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, took := runGateScriptWithin(t, ctx, script, 10*time.Second)
	pid := leftoverPid(t, pidFile)
	t.Logf("returned in %v: %q (error %v)", took, resultText(res), res.IsError)
	if took > 3*time.Second {
		t.Errorf("returned in %v, want within ~3s", took)
	}
	if !strings.Contains(resultText(res), "started") {
		t.Errorf("output %q lost the script's output", resultText(res))
	}
	if !pidGone(pid) {
		t.Errorf("background child %d still running after the call", pid)
	}
}

// A script still running at its timeout: everything it started is stopped and the call returns as an error.
func TestGateScriptTimeoutStopsTheWholeGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := writeGateScript(t, "sleep 300 &\necho $! > "+pidFile+"\necho started\nwait\n")
	killPidFileAtCleanup(t, pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, took := runGateScriptWithin(t, ctx, script, 10*time.Second)
	pid := leftoverPid(t, pidFile)
	t.Logf("returned in %v: %q (error %v)", took, resultText(res), res.IsError)
	if took > 3*time.Second {
		t.Errorf("returned in %v, want within ~3s", took)
	}
	if !res.IsError {
		t.Error("a script stopped at its timeout reported success")
	}
	if !pidGone(pid) {
		t.Errorf("background child %d still running after the timeout", pid)
	}
}

// A script printing far more than the cap: only the cap is kept in memory, then the truncation notice.
func TestGateScriptOutputIsCappedWhileReading(t *testing.T) {
	script := writeGateScript(t, "head -c 50000000 /dev/zero\n")
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res := runGateScript(context.Background(), "gate", script, nil, filepath.Dir(script), nil)
	runtime.ReadMemStats(&after)
	alloc := after.TotalAlloc - before.TotalAlloc
	text := resultText(res)
	t.Logf("output %d bytes, allocated %.1f MiB while running", len(text), float64(alloc)/(1<<20))
	if res.IsError {
		t.Fatalf("error result: %q", text[:min(len(text), 200)])
	}
	if !strings.HasPrefix(text, strings.Repeat("\x00", gateOutputCap)) || !strings.HasSuffix(text, gateOutputNotice) {
		t.Errorf("want the first %d bytes then the notice, got %d bytes", gateOutputCap, len(text))
	}
	if len(text) != gateOutputCap+len(gateOutputNotice) {
		t.Errorf("output %d bytes, want %d", len(text), gateOutputCap+len(gateOutputNotice))
	}
	if alloc > 8<<20 {
		t.Errorf("allocated %d bytes for a 50 MB output, want under 8 MiB", alloc)
	}
}

// Exit codes keep their meaning: a failing script is an error result carrying its output, a short one is not cut.
func TestGateScriptExitStatus(t *testing.T) {
	ok := runGateScript(context.Background(), "gate", writeGateScript(t, "echo \"$1\" \"$PERSONA_GATE_T\"\n"), []string{"hi"}, t.TempDir(), []string{"PERSONA_GATE_T=env"})
	if ok.IsError || resultText(ok) != "hi env\n" {
		t.Errorf("success: %q (error %v)", resultText(ok), ok.IsError)
	}
	bad := runGateScript(context.Background(), "gate", writeGateScript(t, "echo nope >&2\nexit 3\n"), nil, t.TempDir(), nil)
	if !bad.IsError || resultText(bad) != "nope\n" {
		t.Errorf("failure: %q (error %v)", resultText(bad), bad.IsError)
	}
	missing := runGateScript(context.Background(), "gate", writeGateScript(t, "true\n"), nil, filepath.Join(t.TempDir(), "no-such-dir"), nil)
	if !missing.IsError || !strings.HasPrefix(resultText(missing), "gate: ") {
		t.Errorf("start failure: %q (error %v)", resultText(missing), missing.IsError)
	}
}
