//go:build !windows

package herdr

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeFullHerdr stands in for the herdr binary: `session list` names two sessions, `session attach` draws what the
// test checks (its environment, the size of its terminal whenever that changes) and then idles like a TUI.
func fakeFullHerdr(t *testing.T, tail string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
case "$1 $2" in
"session list") printf '{"sessions":[{"name":"default","default":true,"socket_path":"/x/default.sock"},{"name":"lab","default":false,"socket_path":"/x/lab/herdr.sock"}]}' ;;
"session attach")
  echo "pid $$"
  echo "env $TERM|$SSH_CONNECTION|${HERDR_PANE_ID-unset}|${TMUX-unset}|$COLORTERM"
  echo "attach $3"
` + tail + `
  ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

const idleLoop = `  last=""
  while :; do s=$(stty size); if [ "$s" != "$last" ]; then echo "size $s"; last=$s; fi; sleep 0.05; done`

// capture collects a full client's output.
type capture struct {
	mu  sync.Mutex
	buf []byte
}

func (c *capture) add(b []byte) { c.mu.Lock(); c.buf = append(c.buf, b...); c.mu.Unlock() }
func (c *capture) text() string { c.mu.Lock(); defer c.mu.Unlock(); return string(c.buf) }
func (c *capture) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(c.text(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the client's output: %q", want, c.text())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pidIn(t *testing.T, out string) int {
	t.Helper()
	_, rest, ok := strings.Cut(out, "pid ")
	if !ok {
		t.Fatalf("no pid in %q", out)
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("pid in %q: %v", out, err)
	}
	return n
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestFullClientRunsInAPTYAndLeavesNothingBehind(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "w9:p9")
	t.Setenv("TMUX", "/tmp/tmux-0/default,1,0")
	c := &Client{Bin: fakeFullHerdr(t, idleLoop), Socket: "/x/lab/herdr.sock"}
	name, err := c.SessionName(t.Context())
	if err != nil || name != "lab" {
		t.Fatalf("SessionName = %q, %v", name, err)
	}
	var out capture
	fc, err := c.StartFull(FullOptions{Session: name, Cols: 100, Rows: 30}, out.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fc.Close)

	out.waitFor(t, "size 30 100")
	pid := pidIn(t, out.text())
	if !strings.Contains(out.text(), "env xterm-256color|127.0.0.1 1 127.0.0.1 2|unset|unset|truecolor") ||
		!strings.Contains(out.text(), "attach lab") {
		t.Fatalf("the client started as %q", out.text())
	}

	if err := fc.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "size 40 120")
	if err := fc.Write([]byte("typed\n")); err != nil { // the terminal echoes it: it went in
		t.Fatal(err)
	}
	out.waitFor(t, "typed")

	fc.Close()
	select {
	case <-fc.Done():
	default:
		t.Fatal("Close returned before the client was gone")
	}
	if alive(pid) {
		t.Fatalf("client process %d still runs after Close", pid)
	}
	if fc.Err() != nil {
		t.Fatalf("Err after Close = %v", fc.Err())
	}
	if err := fc.Write([]byte("x")); err == nil {
		t.Fatal("wrote to a closed client")
	}
	fc.Close() // again: harmless
}

func TestFullClientCloseEndsAClientThatIgnoresTheHangup(t *testing.T) {
	// A client that ignores HUP and TERM only goes on KILL, which Close sends to the process group.
	c := &Client{Bin: fakeFullHerdr(t, "  trap '' HUP TERM\n  while :; do sleep 0.05; done")}
	var out capture
	fc, err := c.StartFull(FullOptions{Session: "lab", Cols: 80, Rows: 24}, out.add)
	if err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "attach lab")
	pid := pidIn(t, out.text())
	start := time.Now()
	fc.Close()
	if alive(pid) {
		t.Fatalf("client process %d survived Close", pid)
	}
	if d := time.Since(start); d > 9*time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestFullClientQuittingIsReported(t *testing.T) {
	c := &Client{Bin: fakeFullHerdr(t, "  sleep 0.2\n  exit 3")}
	var out capture
	fc, err := c.StartFull(FullOptions{Session: "lab", Cols: 80, Rows: 24}, out.add)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fc.Close)
	select {
	case <-fc.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the client quit and Done did not close")
	}
	if err := fc.Err(); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("Err = %v", err)
	}
}

func TestFullClientNeedsASessionAndASize(t *testing.T) {
	c := &Client{Bin: "herdr"}
	for _, o := range []FullOptions{{Cols: 80, Rows: 24}, {Session: "lab", Rows: 24}, {Session: "lab", Cols: 80}} {
		if _, err := c.StartFull(o, func([]byte) {}); err == nil {
			t.Fatalf("started with %+v", o)
		}
	}
}

func TestSessionNameIn(t *testing.T) {
	list := []byte(`{"sessions":[{"name":"default","default":true,"socket_path":"/home/u/.config/herdr/herdr.sock"},` +
		`{"name":"lab","default":false,"socket_path":"/home/u/.config/herdr/sessions/lab/herdr.sock"}]}`)
	for _, c := range []struct{ socket, want string }{
		{"/home/u/.config/herdr/herdr.sock", "default"},
		{"/home/u/.config/herdr/sessions/lab/herdr.sock", "lab"},
		{"/home/u/.config/herdr/sessions/lab/../lab/herdr.sock", "lab"},
		{"", "default"},
		{"/somewhere/else.sock", ""},
	} {
		got, err := sessionNameIn(list, c.socket)
		if c.want == "" {
			if err == nil || !strings.HasPrefix(err.Error(), "unsupported") {
				t.Fatalf("%q: %q, %v", c.socket, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("%q: %q, %v; want %q", c.socket, got, err, c.want)
		}
	}
	if _, err := sessionNameIn([]byte("not json"), ""); err == nil {
		t.Fatal("read a list that is not JSON")
	}
}

func TestFullClientEnv(t *testing.T) {
	got := fullClientEnv([]string{"PATH=/bin", "HERDR_PANE_ID=w1:p1", "HERDR_SOCKET_PATH=/x", "TMUX=/t", "TERM=dumb", "SSH_CONNECTION=1 2 3 4", "HOME=/h"})
	want := map[string]bool{"PATH=/bin": true, "HOME=/h": true, "TERM=xterm-256color": true, "COLORTERM=truecolor": true, "SSH_CONNECTION=127.0.0.1 1 127.0.0.1 2": true}
	if len(got) != len(want) {
		t.Fatalf("env = %v", got)
	}
	for _, kv := range got {
		if !want[kv] {
			t.Fatalf("unexpected %q in %v", kv, got)
		}
	}
}
