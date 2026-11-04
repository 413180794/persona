package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
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

// A message to Persona' main conversation without a launcher goes out on 设置 › 助手's launcher (it used to keep the
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

// An answer-only device says something to a conversation it can see, no more: not one nobody has (that would start an
// agent), not which program runs it or what else a send can shape, and not keys for the terminal. A full device keeps
// all of those.
func TestAnswerRoleSendsOnlyText(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
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
	m.sessions = []proto.Session{{SessionID: "s1"}, {SessionID: "s2", Status: proto.StatusOffline}}
	m.mu.Unlock()

	mux := h.routes()
	_, phone := call(t, mux, "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"name":"iPhone","kind":"phone"}`)
	_, watch := call(t, mux, "POST", "/api/v1/auth/tokens", phone["token"].(string), `{"name":"Watch","kind":"watch"}`)
	const body = `{"text":"\u0003\u0003hi\r\nthere\t\u001b[201~","launcher":"picked","model":"m","model_fingerprint":"f","images":[{"data":"x"}],"clear_draft":"d","permission_mode":"bypassPermissions"}`
	if code, b := call(t, mux, "POST", "/api/v1/sessions/box/s1/messages", watch["token"].(string), body); code != 200 {
		t.Fatalf("%d %v", code, b)
	}
	mu.Lock()
	sent := got
	got = nil
	mu.Unlock()
	if sent["text"] != "hi\nthere\t[201~" || sent["session_id"] != "s1" {
		t.Fatalf("the watch's message: %q", sent)
	}
	for _, k := range []string{"launcher", "model", "model_fingerprint", "images", "clear_draft", "permission_mode"} {
		if _, ok := sent[k]; ok {
			t.Errorf("an answer-only device's send carries %s: %v", k, sent)
		}
	}
	if code, _ := call(t, mux, "POST", "/api/v1/sessions/box/deadbeef-0000-4000-8000-000000000001/messages", watch["token"].(string), `{"text":"hi"}`); code != http.StatusNotFound {
		t.Fatalf("an answer-only device's message to an id nobody has: %d", code)
	}
	for _, empty := range []string{`{}`, `{"text":"  "}`, `{"text":"\u0003\r"}`, `{"text":5}`} {
		if code, _ := call(t, mux, "POST", "/api/v1/sessions/box/s1/messages", watch["token"].(string), empty); code != http.StatusBadRequest {
			t.Fatalf("an answer-only device's message %s: %d", empty, code)
		}
	}
	if code, b := call(t, mux, "POST", "/api/v1/sessions/box/s2/messages", watch["token"].(string), `{"text":"hi"}`); code != 200 {
		t.Fatalf("an answer-only device's message to a conversation not running: %d %v", code, b)
	}
	if code, b := call(t, mux, "DELETE", "/api/v1/sessions/box/s2", phone["token"].(string), ""); code != 200 {
		t.Fatalf("delete: %d %v", code, b)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/sessions/box/s2/messages", watch["token"].(string), `{"text":"hi"}`); code != http.StatusNotFound {
		t.Fatalf("an answer-only device's message to a deleted conversation: %d", code)
	}
	mu.Lock()
	got = nil
	mu.Unlock()
	mu.Lock()
	sent = got
	mu.Unlock()
	if sent != nil {
		t.Fatalf("reached the node: %v", sent)
	}
	if code, b := call(t, mux, "POST", "/api/v1/sessions/box/deadbeef-0000-4000-8000-000000000001/messages", phone["token"].(string), `{"text":"\u0003hi","launcher":"picked"}`); code != 200 {
		t.Fatalf("%d %v", code, b)
	}
	mu.Lock()
	sent = got
	mu.Unlock()
	if sent["launcher"] != "picked" || sent["text"] != "\u0003hi" {
		t.Fatalf("a full device's send: %q", sent)
	}
}
