//go:build !windows

package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/rpc"
)

const fullTestSocket = "/x/lab/herdr.sock"

// fakeFullHerdrBin stands in for herdr: `session list` names the session the node's socket belongs to, `session attach`
// runs tail in the PTY after saying its pid.
func fakeFullHerdrBin(t *testing.T, tail string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
case "$1 $2" in
"session list") printf '{"sessions":[{"name":"lab","default":true,"socket_path":"` + fullTestSocket + `"}]}' ;;
"session attach")
  echo "pid $$"
` + tail + `
  ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

const (
	fullIdle   = `  while :; do sleep 0.05; done`
	fullFlood  = `  exec yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	fullSizing = `  last=""; while :; do s=$(stty size); if [ "$s" != "$last" ]; then echo "size $s"; last=$s; fi; sleep 0.05; done`
)

// fullHub plays the hub: it records what the node sends about full clients.
type fullHub struct {
	n  *Node
	mu sync.Mutex
	// data by client id, the raw bytes; ends by client id
	data  map[string][]byte
	chunk map[string]int
	ends  map[string]string
}

func newFullHub(t *testing.T, tail string) *fullHub {
	t.Helper()
	n := &Node{agents: nil, state: newState(), hosts: herdrHosts(&herdr.Client{Bin: fakeFullHerdrBin(t, tail), Socket: fullTestSocket})}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	f := &fullHub{n: n, data: map[string][]byte{}, chunk: map[string]int{}, ends: map[string]string{}}
	hub.OnEvent = func(event string, params json.RawMessage) {
		var p struct {
			ClientID string `json:"client_id"`
			Data     string `json:"data"`
			Error    string `json:"error"`
		}
		_ = json.Unmarshal(params, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch event {
		case "herdr.client.data":
			b, _ := base64.StdEncoding.DecodeString(p.Data)
			f.data[p.ClientID] = append(f.data[p.ClientID], b...)
			f.chunk[p.ClientID]++
		case "herdr.client.end":
			f.ends[p.ClientID] = p.Error
		}
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { n.closeFulls(); cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return f
}

func (f *fullHub) open(id string, cols, rows int) error {
	_, err := f.n.fullOpen(context.Background(), json.RawMessage(fmt.Sprintf(`{"client_id":%q,"cols":%d,"rows":%d}`, id, cols, rows)))
	return err
}

func (f *fullHub) event(name, id string, extra string) {
	params := fmt.Sprintf(`{"client_id":%q%s}`, id, extra)
	f.n.onFullEvent(name, json.RawMessage(params))
}

func (f *fullHub) bytes(id string) int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.data[id]) }
func (f *fullHub) text(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.data[id])
}
func (f *fullHub) end(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.ends[id]
	return s, ok
}

func (f *fullHub) waitText(t *testing.T, id, want string) {
	t.Helper()
	fullWait(t, 10*time.Second, fmt.Sprintf("%q from %s (got %q)", want, id, f.text(id)), func() bool { return strings.Contains(f.text(id), want) })
}

func fullWait(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fullHub) pid(t *testing.T, id string) int {
	t.Helper()
	f.waitText(t, id, "pid ")
	fullWait(t, 5*time.Second, "the pid line", func() bool {
		return strings.Contains(f.text(id), "pid ") && strings.Contains(f.text(id)[strings.Index(f.text(id), "pid "):], "\n")
	})
	s := f.text(id)
	s = s[strings.Index(s, "pid ")+4:]
	n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(s, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("pid in %q: %v", s, err)
	}
	return n
}

func gone(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

func TestFullClientStreamsResizesTypesAndStops(t *testing.T) {
	h := newFullHub(t, fullSizing)
	if err := h.open("c1", 90, 28); err != nil {
		t.Fatal(err)
	}
	h.waitText(t, "c1", "size 28 90")
	pid := h.pid(t, "c1")

	h.event("herdr.client.resize", "c1", `,"cols":100,"rows":33`)
	h.waitText(t, "c1", "size 33 100")
	h.event("herdr.client.input", "c1", `,"data":"hello\n"`) // the terminal echoes it
	h.waitText(t, "c1", "hello")
	h.event("herdr.client.input", "c1", `,"b64":"`+base64.StdEncoding.EncodeToString([]byte("bin\n"))+`"`)
	h.waitText(t, "c1", "bin")

	h.event("herdr.client.close", "c1", ``)
	fullWait(t, 15*time.Second, "the client process to go", func() bool { return gone(pid) })
	if n := h.n.fullOf("c1"); n != nil {
		t.Fatal("the client is still registered after close")
	}
	if why, told := h.end("c1"); told {
		t.Fatalf("a close the hub asked for was reported back: %q", why)
	}
}

func TestFullClientBackpressureStopsReadingUntilAcked(t *testing.T) {
	h := newFullHub(t, fullFlood)
	if err := h.open("c1", 100, 30); err != nil {
		t.Fatal(err)
	}
	pid := h.pid(t, "c1")
	// With no ack the node reads at most a window and a chunk, however fast the client draws.
	fullWait(t, 10*time.Second, "the window to fill", func() bool { return h.bytes("c1") >= fullWindow })
	time.Sleep(500 * time.Millisecond)
	got := h.bytes("c1")
	if got > fullWindow+(32<<10) {
		t.Fatalf("%d bytes went out with no ack; the window is %d", got, fullWindow)
	}
	time.Sleep(300 * time.Millisecond)
	if again := h.bytes("c1"); again != got {
		t.Fatalf("still flowing without acks: %d then %d", got, again)
	}

	// An ack for more than was sent counts for nothing (a browser cannot widen the window).
	h.event("herdr.client.ack", "c1", `,"bytes":99999999999`)
	time.Sleep(300 * time.Millisecond)
	if again := h.bytes("c1"); again != got {
		t.Fatalf("an impossible ack opened the window: %d then %d", got, again)
	}

	// A real ack lets it go on, up to the new limit.
	h.event("herdr.client.ack", "c1", fmt.Sprintf(`,"bytes":%d`, got))
	fullWait(t, 10*time.Second, "more after the ack", func() bool { return h.bytes("c1") > got+fullWindow/2 })
	time.Sleep(300 * time.Millisecond)
	if more := h.bytes("c1"); more > 2*fullWindow+(64<<10) {
		t.Fatalf("%d bytes after one ack of %d", more, got)
	}

	h.n.closeFulls()
	fullWait(t, 15*time.Second, "the flooding client to go", func() bool { return gone(pid) })
}

func TestFullClientStalledBrowserEndsIt(t *testing.T) {
	old := fullStall
	fullStall = 300 * time.Millisecond
	t.Cleanup(func() { fullStall = old })
	h := newFullHub(t, fullFlood)
	if err := h.open("c1", 100, 30); err != nil {
		t.Fatal(err)
	}
	pid := h.pid(t, "c1")
	fullWait(t, 15*time.Second, "the end", func() bool { _, ok := h.end("c1"); return ok })
	if why, _ := h.end("c1"); !strings.Contains(why, "stopped reading") {
		t.Fatalf("end = %q", why)
	}
	fullWait(t, 15*time.Second, "the process to go", func() bool { return gone(pid) })
}

func TestFullClientLeaseRunsOut(t *testing.T) {
	old := fullLease
	fullLease = 500 * time.Millisecond
	t.Cleanup(func() { fullLease = old })
	h := newFullHub(t, fullIdle)
	if err := h.open("kept", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := h.open("lost", 80, 24); err != nil {
		t.Fatal(err)
	}
	lostPid := h.pid(t, "lost")
	// The hub's keepalive names the one it still has; the other one's lease runs out.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				h.n.onFullEvent("herdr.client.keepalive", json.RawMessage(`{"client_ids":["kept"]}`))
			}
		}
	}()
	fullWait(t, 15*time.Second, "the lost client's end", func() bool { _, ok := h.end("lost"); return ok })
	if why, _ := h.end("lost"); !strings.Contains(why, "renewing") {
		t.Fatalf("end = %q", why)
	}
	fullWait(t, 15*time.Second, "the lost process to go", func() bool { return gone(lostPid) })
	if _, ok := h.end("kept"); ok || h.n.fullOf("kept") == nil {
		t.Fatal("the client with a fresh lease was closed")
	}
}

func TestFullClientTheClientQuittingIsReported(t *testing.T) {
	h := newFullHub(t, "  sleep 0.3\n  exit 2")
	if err := h.open("c1", 80, 24); err != nil {
		t.Fatal(err)
	}
	fullWait(t, 15*time.Second, "the end", func() bool { _, ok := h.end("c1"); return ok })
	if why, _ := h.end("c1"); !strings.Contains(why, "exited") {
		t.Fatalf("end = %q", why)
	}
	if h.n.fullOf("c1") != nil {
		t.Fatal("a client that quit is still registered")
	}
}

func TestFullClientCapAndBadRequests(t *testing.T) {
	h := newFullHub(t, fullIdle)
	var pids []int
	for i := 0; i < fullMaxClients; i++ {
		id := fmt.Sprintf("c%d", i)
		if err := h.open(id, 80, 24); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
		pids = append(pids, h.pid(t, id))
	}
	err := h.open("one-too-many", 80, 24)
	var rerr *rpc.Error
	if !errors.As(err, &rerr) || rerr.Code != "too_many" {
		t.Fatalf("fifth open = %v", err)
	}
	if err := h.open("c0", 80, 24); err == nil {
		t.Fatal("opened a client id twice")
	}
	for _, bad := range []struct {
		id         string
		cols, rows int
	}{{"", 80, 24}, {strings.Repeat("x", 65), 80, 24}, {"z", 0, 24}, {"z", 80, 0}, {"z", fullMaxCols + 1, 24}, {"z", 80, fullMaxRows + 1}} {
		if err := h.open(bad.id, bad.cols, bad.rows); err == nil {
			t.Fatalf("opened %+v", bad)
		}
	}
	// A client closed makes room for another.
	h.event("herdr.client.close", "c0", ``)
	fullWait(t, 15*time.Second, "room", func() bool { return h.open("again", 80, 24) == nil })
	h.pid(t, "again")

	// The hub connection ending ends them all: none is left running.
	h.n.closeFulls()
	for _, pid := range pids {
		fullWait(t, 15*time.Second, "the process to go", func() bool { return gone(pid) })
	}
	if len(h.n.fulls) != 0 {
		t.Fatalf("clients left after closeFulls: %d", len(h.n.fulls))
	}
}

func TestFullClientEventsForOtherClientsAreIgnored(t *testing.T) {
	h := newFullHub(t, fullIdle)
	if !h.n.onFullEvent("herdr.client.input", json.RawMessage(`{"client_id":"nobody","data":"x"}`)) {
		t.Fatal("a full client event was not recognised")
	}
	if h.n.onFullEvent("terminal.input", json.RawMessage(`{}`)) {
		t.Fatal("a terminal event was taken as a full client event")
	}
}

func TestFullClientWithoutAHerdrSessionIsUnsupported(t *testing.T) {
	n := &Node{state: newState(), hosts: herdrHosts(&herdr.Client{Bin: fakeFullHerdrBin(t, fullIdle), Socket: "/not/a/session.sock"})}
	_, err := n.fullOpen(context.Background(), json.RawMessage(`{"client_id":"c","cols":80,"rows":24}`))
	if err == nil || !strings.HasPrefix(err.Error(), "unsupported") {
		t.Fatalf("open = %v", err)
	}
}
