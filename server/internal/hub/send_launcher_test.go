package hub

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/chyroc/jarvis/server/internal/rpc"
)

// assistantStore answers the "assistant" setting with cfg.
type assistantStore struct {
	*memStore
	cfg AssistantConfig
}

func (s *assistantStore) Setting(_ context.Context, key string, out any) (bool, error) {
	if key != "assistant" {
		return false, nil
	}
	b, _ := json.Marshal(s.cfg)
	return true, json.Unmarshal(b, out)
}

// A message to Jarvis' main conversation without a launcher goes out on 设置 › 助手's launcher (it used to keep the
// one it started with, so switching seemed to do nothing); a launcher the app picked wins; other conversations are
// left alone.
func TestMainConversationFollowsTheAssistantLauncher(t *testing.T) {
	h := newTestHub(t)
	h.store = &assistantStore{memStore: newMemStore(), cfg: AssistantConfig{MachineID: "box", Launcher: "ccc", Main: "main-1"}}
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	h.byID["box"] = m
	h.machines = append(h.machines, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	var mu sync.Mutex
	var got map[string]any
	nodePeer.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method == "session.send" {
			mu.Lock()
			got = map[string]any{}
			_ = json.Unmarshal(params, &got)
			mu.Unlock()
		}
		return map[string]any{"ok": true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()

	mux := h.routes()
	for _, c := range []struct{ sid, body, want string }{
		{"main-1", `{"text":"hi"}`, "ccc"},
		{"main-1", `{"text":"hi","launcher":"other"}`, "other"},
		{"side-2", `{"text":"hi"}`, ""},
	} {
		if code, out := call(t, mux, "POST", "/api/v1/sessions/box/"+c.sid+"/messages", h.cfg.Token, c.body); code != 200 {
			t.Fatalf("%s: %d %v", c.sid, code, out)
		}
		mu.Lock()
		l, _ := got["launcher"].(string)
		mu.Unlock()
		if l != c.want {
			t.Errorf("%s %s: launcher %q, want %q", c.sid, c.body, l, c.want)
		}
	}
}
