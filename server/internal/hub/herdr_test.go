package hub

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// herdrMac is a machine whose node answers node.configure and herdr.server.start the way the node does, and says
// which it was asked (herdrstart.go in the node).
func herdrMac(t *testing.T, h *Hub, id string, starts bool) (*Machine, func() []map[string]any) {
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
		}
		return nil, &rpc.Error{Message: "unknown method " + method}
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	m.peer, m.online, m.info = hubSide, true, proto.NodeInfo{OS: "linux", HerdrRunning: false, HerdrRunningKnown: true}
	h.mu.Lock()
	h.machines, h.byID[id] = append(h.machines, m), m
	h.mu.Unlock()
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
	h, _ := extensionTestHub(t)
	off := false
	m, seen := herdrMac(t, h, "mac", false)
	m.cfg.HerdrAutostart = &off // hub.json says off; nothing chosen for this machine yet
	mux := h.routes()

	code, out := call(t, mux, "GET", "/api/v1/machines/mac/herdr", h.cfg.Token, "")
	if code != 200 || out["autostart"] != false || out["running"] != false || out["known"] != true {
		t.Fatalf("hub.json's value is the default: %d %v", code, out)
	}

	code, out = call(t, mux, "PUT", "/api/v1/machines/mac/herdr", h.cfg.Token, `{"autostart": true}`)
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
	code, out = call(t, mux, "GET", "/api/v1/machines/mac/herdr", h.cfg.Token, "")
	if code != 200 || out["autostart"] != true {
		t.Fatalf("stored choice wins over hub.json: %d %v", code, out)
	}

	// And it is in the settings store, so a restarted hub reads it back rather than the default.
	var stored bool
	if ok, err := h.store.Setting(context.Background(), herdrAutostartKey("mac"), &stored); !ok || err != nil || !stored {
		t.Fatalf("kept in settings: ok=%v err=%v value=%v", ok, err, stored)
	}
}

// The 启动 button works with autostart off — the switch says whether the node starts herdr on its own.
func TestHerdrStartButton(t *testing.T) {
	h, _ := extensionTestHub(t)
	off := false
	m, seen := herdrMac(t, h, "mac", true)
	m.cfg.HerdrAutostart = &off
	mux := h.routes()

	code, out := call(t, mux, "POST", "/api/v1/machines/mac/herdr/start", h.cfg.Token, "")
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

// A machine driving something else has no such row.
func TestHerdrRowOnlyForAHerdrEngine(t *testing.T) {
	h, _ := extensionTestHub(t)
	herdrMac(t, h, "mac", true)
	mux := h.routes()
	h.engines.mu.Lock()
	if h.engines.byID == nil {
		h.engines.byID = map[string]string{}
	}
	h.engines.byID["mac"] = proto.EngineHerdrBoth
	h.engines.loaded = true
	h.engines.mu.Unlock()
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/mac/herdr", h.cfg.Token, `{"autostart": false}`); code != 200 {
		t.Fatalf("herdr-both has the row: %d", code)
	}

	herdrMac(t, h, "orca-box", true)
	h.engines.mu.Lock()
	h.engines.byID["orca-box"] = proto.EngineOrca
	h.engines.mu.Unlock()
	if code, _ := call(t, mux, "PUT", "/api/v1/machines/orca-box/herdr", h.cfg.Token, `{"autostart": false}`); code != 404 {
		t.Fatalf("an orca machine has no herdr row: %d", code)
	}
}
