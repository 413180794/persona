package devices

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// herdrMac is a machine whose node answers node.configure and herdr.server.start the way the node does, and says
// which it was asked (herdrstart.go in the node).
func herdrMac(t *testing.T, h *Devices, id string, starts bool) (*Machine, func() []map[string]any) {
	t.Helper()
	return herdrMacStatus(t, h, id, starts, nil)
}

// herdrMacStatus is herdrMac whose node also answers herdr.server.status with running (nil: an older node, which
// has no such method).
func herdrMacStatus(t *testing.T, h *Devices, id string, starts bool, running *atomic.Bool) (*Machine, func() []map[string]any) {
	t.Helper()
	return herdrMacInstall(t, h, id, starts, running, nil)
}

// herdrMacInstall is herdrMacStatus whose node's status also says it is installing herdr (install, when not nil)
// and why its last install failed.
func herdrMacInstall(t *testing.T, h *Devices, id string, starts bool, running *atomic.Bool, install *atomic.Pointer[string]) (*Machine, func() []map[string]any) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	var mu sync.Mutex
	var seen []map[string]any
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		p["method"] = method
		seen = append(seen, p)
		switch method {
		case "node.configure":
			return map[string]any{}, nil
		case "herdr.server.start":
			return map[string]any{"running": starts}, nil
		case "herdr.server.status":
			if running != nil {
				out := map[string]any{"running": running.Load(), "known": true}
				if install != nil {
					if e := install.Load(); e != nil {
						out["installing"], out["install_error"] = *e == "", *e
					}
				}
				return out, nil
			}
		}
		return nil, &rpc.Error{Message: "unknown method " + method}
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online, m.info = hubSide, true, proto.NodeInfo{OS: "linux", HerdrRunning: false, HerdrRunningKnown: true}
	h.addEntry(m)
	return m, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), seen...)
	}
}

// autostartCalls counts the node.configure calls that said anything about herdr_autostart.
func autostartCalls(seen []map[string]any) []bool {
	var out []bool
	for _, c := range seen {
		if c["method"] != "node.configure" {
			continue
		}
		if v, ok := c["herdr_autostart"].(bool); ok {
			out = append(out, v)
		}
	}
	return out
}

// The switch answers for one machine: hub.json is only its default, the choice survives a restart of the hub, and the
// node is told as soon as it is made.
func TestHerdrAutostartPerMachine(t *testing.T) {
	h := newTestDevices(t)
	off := false
	m, seen := herdrMac(t, h, "mac", false)
	m.cfg.HerdrAutostart = &off // hub.json says off; nothing chosen for this machine yet
	mux := h.Handler(http.NotFoundHandler())

	code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", "")
	if code != 200 || out["autostart"] != false || out["running"] != false || out["known"] != true {
		t.Fatalf("hub.json's value is the default: %d %v", code, out)
	}

	code, out = call(t, mux, "PUT", "/api/v1/machines/mac/herdr", `{"autostart": true}`)
	if code != 200 || out["autostart"] != true {
		t.Fatalf("turned on: %d %v", code, out)
	}
	// The node hears about it now, not at its next connection.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := autostartCalls(seen()); len(got) > 0 && got[len(got)-1] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the node was not told: %v", autostartCalls(seen()))
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The choice is the machine's, not hub.json's: a second read after the decision answers true even though the
	// config still says off.
	code, out = call(t, mux, "GET", "/api/v1/machines/mac/herdr", "")
	if code != 200 || out["autostart"] != true {
		t.Fatalf("stored choice wins over hub.json: %d %v", code, out)
	}

	// And it is in the settings store, so a restarted hub reads it back rather than the default.
	var stored bool
	if ok, err := h.opts.Store.Setting(context.Background(), herdrAutostartKey("mac"), &stored); !ok || err != nil || !stored {
		t.Fatalf("kept in settings: ok=%v err=%v value=%v", ok, err, stored)
	}
}

// The "Start" button works with autostart off — the switch says whether the node starts herdr on its own.
func TestHerdrStartButton(t *testing.T) {
	h := newTestDevices(t)
	off := false
	m, seen := herdrMac(t, h, "mac", true)
	m.cfg.HerdrAutostart = &off
	mux := h.Handler(http.NotFoundHandler())

	code, out := call(t, mux, "POST", "/api/v1/machines/mac/herdr/start", "")
	if code != 200 || out["running"] != true || out["known"] != true {
		t.Fatalf("start: %d %v", code, out)
	}
	var asked bool
	for _, c := range seen() {
		if c["method"] == "herdr.server.start" {
			asked = true
		}
		if c["method"] == "node.configure" {
			if v, ok := c["herdr_autostart"].(bool); ok && v {
				t.Fatal("pressing 启动 must not flip the switch")
			}
		}
	}
	if !asked {
		t.Fatalf("the node was not asked to start it: %v", seen())
	}
}

// The row says whether herdr answers now, not what node.info said when the node connected: after a reboot the node
// starts herdr itself once the hub's node.configure came, which is after node.info (#1916).
func TestHerdrRowAsksTheNode(t *testing.T) {
	h := newTestDevices(t)
	var running atomic.Bool
	running.Store(true) // node.info said down (herdrMac); it came up since
	_, seen := herdrMacStatus(t, h, "mac", true, &running)
	mux := h.Handler(http.NotFoundHandler())
	asked := func() int {
		n := 0
		for _, c := range seen() {
			if c["method"] == "herdr.server.status" {
				n++
			}
		}
		return n
	}

	code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", "")
	if code != 200 || out["running"] != true || out["known"] != true {
		t.Fatalf("the node's answer: %d %v", code, out)
	}
	// Every Agents list polls: a read right after is answered from what the node just said.
	running.Store(false)
	if _, out = call(t, mux, "GET", "/api/v1/machines/mac/herdr", ""); out["running"] != true || asked() != 1 {
		t.Fatalf("within herdrStatusFresh: %v, asked %d times", out, asked())
	}
	prev := herdrStatusFresh
	herdrStatusFresh = 0
	t.Cleanup(func() { herdrStatusFresh = prev })
	if _, out = call(t, mux, "GET", "/api/v1/machines/mac/herdr", ""); out["running"] != false || asked() != 2 {
		t.Fatalf("stopped since: %v, asked %d times", out, asked())
	}
}

// A "Start" on a machine without herdr goes on installing it in the background (node/herdrinstall.go): the row reads
// installing from the node, then why it failed; hub.json's herdr_autoinstall reaches the node in node.configure.
func TestHerdrRowInstalling(t *testing.T) {
	h := newTestDevices(t)
	var running atomic.Bool
	var install atomic.Pointer[string]
	installing := ""
	install.Store(&installing)
	m, seen := herdrMacInstall(t, h, "mac", false, &running, &install)
	prev := herdrStatusFresh
	herdrStatusFresh = 0
	t.Cleanup(func() { herdrStatusFresh = prev })
	mux := h.Handler(http.NotFoundHandler())

	code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", "")
	if code != 200 || out["installing"] != true || out["running"] != false {
		t.Fatalf("installing: %d %v", code, out)
	}
	failed := "没能下载 herdr 0.8.0：连不上 github.com"
	install.Store(&failed)
	if _, out = call(t, mux, "GET", "/api/v1/machines/mac/herdr", ""); out["installing"] != nil || out["install_error"] != failed {
		t.Fatalf("failed: %v", out)
	}

	m.cfg.HerdrAutoinstall = true
	m.configureNode(context.Background(), m.peer, nil, true)
	var said bool
	for _, c := range seen() {
		if c["method"] == "node.configure" && c["herdr_autoinstall"] == true {
			said = true
		}
	}
	if !said {
		t.Fatalf("node.configure without herdr_autoinstall: %v", seen())
	}
}

// An older node has no herdr.server.status: the row says what node.info said, and the node is not asked on every read.
func TestHerdrRowOlderNode(t *testing.T) {
	h := newTestDevices(t)
	_, seen := herdrMac(t, h, "mac", true)
	mux := h.Handler(http.NotFoundHandler())
	for range 3 {
		if code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", ""); code != 200 || out["running"] != false || out["known"] != true {
			t.Fatalf("node.info's answer: %d %v", code, out)
		}
	}
	n := 0
	for _, c := range seen() {
		if c["method"] == "herdr.server.status" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("asked %d times in a row", n)
	}
}

// After "Start" the row says it runs, even from an older node that cannot be asked again: it said so starting it.
func TestHerdrRowAfterStart(t *testing.T) {
	h := newTestDevices(t)
	herdrMac(t, h, "mac", true)
	mux := h.Handler(http.NotFoundHandler())
	if code, out := call(t, mux, "POST", "/api/v1/machines/mac/herdr/start", ""); code != 200 || out["running"] != true {
		t.Fatalf("start: %d %v", code, out)
	}
	prev := herdrStatusFresh
	herdrStatusFresh = 0 // asked (and refused) on this read: the start's answer stays
	t.Cleanup(func() { herdrStatusFresh = prev })
	if code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", ""); code != 200 || out["running"] != true || out["known"] != true {
		t.Fatalf("after the start: %d %v", code, out)
	}
}

// A machine driving something else has no such row.
func TestHerdrRowOnlyForAHerdrEngine(t *testing.T) {
	h := newTestDevices(t)
	herdrMac(t, h, "mac", true)
	mux := h.Handler(http.NotFoundHandler())
	h.engines.mu.Lock()
	if h.engines.byID == nil {
		h.engines.byID = map[string]string{}
	}
	h.engines.byID["mac"] = proto.EngineHerdrBoth
	h.engines.loaded = true
	h.engines.mu.Unlock()
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/mac/herdr", `{"autostart": false}`); code != 200 {
		t.Fatalf("herdr-both has the row: %d", code)
	}

	herdrMac(t, h, "orca-box", true)
	h.engines.mu.Lock()
	h.engines.byID["orca-box"] = proto.EngineOrca
	h.engines.mu.Unlock()
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/orca-box/herdr", `{"autostart": false}`); code != 404 {
		t.Fatalf("an orca machine has no herdr row: %d", code)
	}
}

// call serves one request on mux and decodes its JSON answer.
func call(t *testing.T, mux http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}
