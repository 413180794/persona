package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// settingsStore keeps settings in memory like the settings table.
type settingsStore struct {
	Store
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

func (s *settingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *settingsStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	s.puts++
	return nil
}

// engineNode is a node answering the hub over pipes, recording the methods it was called with.
type engineNode struct {
	mu    sync.Mutex
	calls []string
}

func (f *engineNode) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+string(params))
	f.mu.Unlock()
	if method == "sessions.list" {
		return map[string]any{"sessions": []proto.Session{}}, nil
	}
	return map[string]any{}, nil
}

func (f *engineNode) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func engineHub(t *testing.T) (*Hub, *settingsStore, *Machine, http.Handler) {
	t.Helper()
	h := newTestHub(t)
	st := &settingsStore{vals: map[string][]byte{}}
	h.store = st
	m := newMachine(h, MachineConfig{ID: "m1", Name: "box"})
	h.byID["m1"], h.machines = m, []*Machine{m}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/machines/{mid}/engine", h.withMachine(h.putEngine))
	mux.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"machines": h.machineList()})
	})
	return h, st, m, mux
}

// connectEngineNode gives m a node connection.
func connectEngineNode(t *testing.T, m *Machine) (*engineNode, *rpc.Peer) {
	t.Helper()
	f := &engineNode{}
	toNode, nodeIn := io.Pipe()
	toHub, hubIn := io.Pipe()
	hubPeer := rpc.New("h", nodeIn)
	nodePeer := rpc.New("n", hubIn)
	nodePeer.OnRequest = f.handle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); toNode.Close(); toHub.Close() })
	go hubPeer.Serve(ctx, toHub)
	go nodePeer.Serve(ctx, toNode)
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.info.Engines = []proto.Engine{{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"},
		{ID: "herdr-core", Name: "Herdr App", Available: true, Version: "0.1.16"}}
	m.mu.Unlock()
	return f, hubPeer
}

func put(t *testing.T, mux http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("PUT", path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Offline: herdr by default, engines empty; a PUT is stored (settings, one key) and answered with the machine; bad
// engines and unknown machines are refused.
func TestEnginePutOffline(t *testing.T) {
	h, st, _, mux := engineHub(t)
	ms := h.machineList()
	b, _ := json.Marshal(ms[0])
	if ms[0].Engine != "herdr" || !strings.Contains(string(b), `"engine":"herdr","engines":[]`) {
		t.Errorf("default machine: %s", b)
	}
	code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"herdr-core"}`)
	if code != 200 || out["engine"] != "herdr-core" || out["id"] != "m1" {
		t.Fatalf("PUT = %d %v", code, out)
	}
	var stored map[string]string
	if ok, _ := st.Setting(context.Background(), enginesKey, &stored); !ok || stored["m1"] != "herdr-core" {
		t.Errorf("stored %v", stored)
	}
	if code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"tmux"}`); code != 400 || out["error"] == nil {
		t.Errorf("unknown engine: %d %v", code, out)
	}
	if code, _ := put(t, mux, "/api/v1/machines/m1/engine", `nope`); code != 400 {
		t.Errorf("bad body: %d", code)
	}
	if code, _ := put(t, mux, "/api/v1/machines/zz/engine", `{"engine":"herdr"}`); code != 404 {
		t.Errorf("unknown machine: %d", code)
	}
	if st.puts != 1 {
		t.Errorf("%d writes", st.puts)
	}

	// A new hub reads it back from settings.
	h2 := newTestHub(t)
	h2.store = st
	if got := h2.engineOf("m1"); got != "herdr-core" {
		t.Errorf("after a restart: %s", got)
	}
	if got := h2.engineOf("other"); got != "herdr" {
		t.Errorf("never set: %s", got)
	}
}

// Online: a PUT is sent to the node at once, the machine lists what the node found; on connect engine.use goes before
// anything else (configureNode runs before the first session list).
func TestEngineOnline(t *testing.T) {
	_, _, m, mux := engineHub(t)
	f, peer := connectEngineNode(t, m)
	code, out := put(t, mux, "/api/v1/machines/m1/engine", `{"engine":"herdr-core"}`)
	if code != 200 || out["engine"] != "herdr-core" {
		t.Fatalf("PUT = %d %v", code, out)
	}
	if calls := f.seen(); len(calls) != 1 || calls[0] != `engine.use {"engine":"herdr-core"}` {
		t.Errorf("node got %v", calls)
	}
	engines, _ := out["engines"].([]any)
	if len(engines) != 2 || engines[1].(map[string]any)["name"] != "Herdr App" {
		t.Errorf("engines %v", out["engines"])
	}

	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
	m.configureNode(context.Background(), peer)
	if calls := f.seen(); len(calls) == 0 || calls[0] != `engine.use {"engine":"herdr-core"}` {
		t.Errorf("on connect the node got %v", calls)
	}
}
