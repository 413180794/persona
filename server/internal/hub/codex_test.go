package hub

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestCodexHTTPUnknownDeliverySurvivesReconnect(t *testing.T) {
	h := newTestHub(t)
	h.store = newSentStore()
	var sends atomic.Int32
	pipeNode(t, h, "box", func(context.Context) error {
		sends.Add(1)
		return &rpc.Error{Code: "delivery_unknown", Message: "synthetic lost response", Data: json.RawMessage(`{"delivered":"unknown"}`)}
	})
	m := h.byID["box"]
	m.mu.Lock()
	m.sessions = []proto.Session{{SessionID: "s1", SourceKind: "codex_app_server", Capabilities: &proto.SessionCapabilities{Send: true}}}
	m.mu.Unlock()
	body := `{"text":"合成请求","client_msg_id":"codex-message"}`
	for range 2 {
		code, _ := call(t, h.routes(), "POST", sendPath, h.cfg.Token, body)
		if code == 200 {
			t.Fatal("unknown send reported delivered")
		}
	}
	// A new hub has no in-memory flight; the persisted operation still prevents a second send.
	h.sends = sendLog{}
	code, _ := call(t, h.routes(), "POST", sendPath, h.cfg.Token, body)
	if code == 200 || sends.Load() != 1 {
		t.Fatalf("after reconnect: code %d sends %d", code, sends.Load())
	}
}

func TestCodexHTTPRequiresDurableOperation(t *testing.T) {
	h := newTestHub(t)
	h.store = nil
	var sends atomic.Int32
	pipeNode(t, h, "box", func(context.Context) error { sends.Add(1); return nil })
	m := h.byID["box"]
	m.mu.Lock()
	m.sessions = []proto.Session{{SessionID: "s1", SourceKind: "codex_app_server"}}
	m.mu.Unlock()
	for _, body := range []string{`{"text":"synthetic"}`, `{"text":"synthetic","client_msg_id":"one"}`} {
		code, _ := call(t, h.routes(), "POST", sendPath, h.cfg.Token, body)
		if code == 200 {
			t.Fatal("unguarded send accepted")
		}
	}
	if sends.Load() != 0 {
		t.Fatal("sent without deduplication guard")
	}
}

func TestCodexInventoryRememberedOutsideNodeSessions(t *testing.T) {
	h := newTestHub(t)
	h.store = &settingsStore{vals: map[string][]byte{}}
	m := newMachine(h, MachineConfig{ID: "box", CodexHomes: []string{t.TempDir()}})
	cfg := proto.CodexConfig{LegacyHome: t.TempDir(), Homes: []string{t.TempDir()}, Joined: map[string]string{"c0de-11111111222222223333333344444444": "one"}}
	raw, _ := json.Marshal(cfg)
	m.rememberCodexInventory(raw)
	restored := m.codexConfig(context.Background())
	if restored.LegacyHome != cfg.LegacyHome || len(restored.Homes) != 2 || len(restored.Joined) != 1 {
		t.Fatalf("inventory not retained: %+v", restored)
	}
}
