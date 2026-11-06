//go:build !windows

package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/osproc"
)

// TestHelperLCUProcess is not a test: run by the tests below as a stand-in `lcu`, it answers initialize and every
// tools/call on stdio and exits when stdin closes.
func TestHelperLCUProcess(t *testing.T) {
	if os.Getenv("PERSONA_LCU_HELPER") != "1" {
		t.Skip("helper process")
	}
	br := bufio.NewReader(os.Stdin)
	for {
		raw, err := readMCP(br)
		if err != nil {
			os.Exit(0)
		}
		var msg mcpMessage
		if json.Unmarshal(raw, &msg) != nil || msg.ID == nil {
			continue
		}
		var result json.RawMessage
		switch msg.Method {
		case "initialize":
			result = json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{}}`)
		case "tools/call":
			result = json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":"pid %d"}]}`, os.Getpid()))
		default:
			continue
		}
		frame, _ := json.Marshal(mcpMessage{JSONRPC: "2.0", ID: msg.ID, Result: result})
		if msg.Method == "tools/call" && os.Getenv("PERSONA_LCU_HELPER_ORPHAN") == "1" {
			// It ends after this answer while a child of its own keeps its stdout open, so its exit is not seen until
			// the child is gone. Its stdin closes first: the next write to it fails.
			_ = os.Stdin.Close()
			child := exec.Command("sleep", "5")
			child.Stdout = os.Stdout
			_ = child.Start()
			fmt.Fprintf(os.Stdout, "%s\n", frame)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stdout, "%s\n", frame)
	}
}

// standInLCU points lcuCommand at bin/args for the test and counts the starts.
func standInLCU(t *testing.T, starts *atomic.Int32, bin string, args ...string) {
	t.Helper()
	old := lcuCommand
	t.Cleanup(func() { lcuCommand = old })
	lcuCommand = func() (string, []string) {
		starts.Add(1)
		return bin, args
	}
}

func useHelperLCU(t *testing.T, starts *atomic.Int32) {
	t.Setenv("PERSONA_LCU_HELPER", "1")
	standInLCU(t, starts, os.Args[0], "-test.run=^TestHelperLCUProcess$")
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the stand-in never wrote its pid")
	return 0
}

func waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !osproc.Alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// An lcu that dies on start fails the first call at once with the process error, not after the 30 s handshake wait.
func TestLCUProcessThatDiesOnStartFailsAtOnce(t *testing.T) {
	var starts atomic.Int32
	standInLCU(t, &starts, "/bin/sh", "-c", "exit 3")
	sess := newLCUSession(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	began := time.Now()
	_, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "LCU 的进程结束了") {
		t.Fatalf("err = %v, want the process ended", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("took %v to notice the process ended", took)
	}
}

// An lcu that ends while a child of its own keeps its stdout open, so its exit is not seen yet, fails the next call at
// once as ended (no wait for the exit), and the call after that starts a new one.
func TestLCUThatEndsBehindAChildFailsAtOnceAndRestarts(t *testing.T) {
	var starts atomic.Int32
	useHelperLCU(t, &starts)
	t.Setenv("PERSONA_LCU_HELPER_ORPHAN", "1")
	sess := newLCUSession(nil)
	defer stopLCU(sess)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", "")
	if err != nil || !strings.HasPrefix(first.Text, "pid ") {
		t.Fatalf("first call: %+v %v", first, err)
	}
	t.Setenv("PERSONA_LCU_HELPER_ORPHAN", "")
	began := time.Now()
	if _, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", ""); !errors.Is(err, errLCUEnded) {
		t.Fatalf("err = %v, want the process ended", err)
	}
	if took := time.Since(began); took > time.Second {
		t.Fatalf("took %v to say the process ended", took)
	}
	next, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", "")
	if err != nil || !strings.HasPrefix(next.Text, "pid ") || next.Text == first.Text || starts.Load() != 2 {
		t.Fatalf("the call after: %+v %v, %d starts", next, err, starts.Load())
	}
}

// A handshake that fails stops the process it started, and the next call starts a working one.
func TestLCUFailedHandshakeStopsTheProcess(t *testing.T) {
	var starts atomic.Int32
	pidfile := filepath.Join(t.TempDir(), "pid")
	standInLCU(t, &starts, "/bin/sh", "-c", `echo $$ > "$0"; exec sleep 300`, pidfile)
	sess := newLCUSession(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", ""); err == nil {
		t.Fatal("a stand-in that never answers must fail the handshake")
	}
	pid := readPID(t, pidfile)
	if !waitGone(pid, 2*time.Second) {
		_ = osproc.Kill(pid)
		t.Fatalf("lcu pid %d still runs after its handshake failed", pid)
	}

	useHelperLCU(t, &starts)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	res, err := sess.call(ctx2, "js", map[string]any{"code": "return 1"}, "", "")
	if err != nil || !strings.HasPrefix(res.Text, "pid ") {
		t.Fatalf("second call: %+v %v", res, err)
	}
	stopLCU(sess)
}

// The late exit of a replaced lcu does not mark the fresh session failed: the next call reuses the running process.
func TestLCUOldProcessExitDoesNotFailTheNewSession(t *testing.T) {
	var starts atomic.Int32
	// The background sleep keeps stdout open past the kill, so this process's exit lands after the restart.
	standInLCU(t, &starts, "/bin/sh", "-c", "sleep 3 & exec sleep 300")
	sess := newLCUSession(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := sess.call(ctx, "js", map[string]any{"code": "return 1"}, "", ""); err == nil {
		t.Fatal("a stand-in that never answers must fail the handshake")
	}

	useHelperLCU(t, &starts)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	first, err := sess.call(ctx2, "js", map[string]any{"code": "return 1"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second) // the first process's stdout closes, and its exit is seen
	again, err := sess.call(ctx2, "js", map[string]any{"code": "return 1"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Text != first.Text || starts.Load() != 2 {
		t.Fatalf("the healthy session was restarted: %q then %q, %d starts", first.Text, again.Text, starts.Load())
	}
	stopLCU(sess)
}

// stopLCU ends the session's process at the end of a test.
func stopLCU(s *lcuSession) {
	s.mu.Lock()
	cmd, stdin := s.cmd, s.stdin
	s.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
