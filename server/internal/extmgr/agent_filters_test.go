package extmgr

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// filtersNode is a node that records the agent_filters of each node.configure it gets.
type filtersNode struct {
	mu   sync.Mutex
	sent [][]proto.AgentFilter
}

func connectFiltersNode(t *testing.T, h *Manager, id string) (*Machine, *filtersNode) {
	t.Helper()
	f := &filtersNode{}
	m := newMachine(h, MachineConfig{ID: id, Name: id})
	h.byID[id] = m
	h.machines = append(h.machines, m)
	hubIn, nodeOut := io.Pipe()
	nodeIn, hubOut := io.Pipe()
	hubPeer, nodePeer := rpc.New("h", hubOut), rpc.New("n", nodeOut)
	nodePeer.OnRequest = func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		var c map[string]json.RawMessage
		_ = json.Unmarshal(raw, &c)
		if method == "node.configure" && c["agent_filters"] != nil {
			var fs []proto.AgentFilter
			_ = json.Unmarshal(c["agent_filters"], &fs)
			f.mu.Lock()
			f.sent = append(f.sent, fs)
			f.mu.Unlock()
		}
		return map[string]any{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = hubOut.Close(); _ = nodeOut.Close() })
	go func() { _ = hubPeer.Serve(ctx, hubIn) }()
	go func() { _ = nodePeer.Serve(ctx, nodeIn) }()
	m.mu.Lock()
	m.peer, m.online = hubPeer, true
	m.mu.Unlock()
	return m, f
}

func waitSent(t *testing.T, f *filtersNode, n int) [][]proto.AgentFilter {
	t.Helper()
	sent := func() [][]proto.AgentFilter {
		f.mu.Lock()
		defer f.mu.Unlock()
		return slices.Clone(f.sent)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(sent()) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := sent()
	if len(got) != n {
		t.Fatalf("node.configure with agent_filters %d times, want %d: %v", len(got), n, got)
	}
	return got
}

// The filters handed to the nodes follow the extension's setting: on by its default, off once the setting is saved
// false, and gone with the extension switched off. The contributions list shows the filter's switch as the setting's.
func TestAgentFiltersFollowTheSetting(t *testing.T) {
	h, _ := extensionTestHub(t)
	m, node := connectFiltersNode(t, h, "m1")
	// The manager's part of a node's new connection (devices.Options.Own: added to its node.configure).
	if own := h.Own(context.Background(), m.cfg.ID); string(own["agent_filters"]) != "[]" {
		t.Fatalf("a new connection is told of none before the extension: %s", own["agent_filters"])
	}
	mux := h.routes()
	master := h.cfg.Token
	dir := declarativePackage(t, `{"manifest_version": 1, "id": "bots", "name": "Bots", "version": "1.0.0",
		"configuration": {"type": "object", "properties": {"hide_agents": {"type": "boolean", "title": "隐藏", "default": true}}},
		"contributes": {"agent_filters": [{"id": "runs", "title": "机器人的会话", "when": "hide_agents", "agents": ["codex"], "originators": ["codex-bot"]}]}}`)
	installForTest(t, h, dir)

	got := h.agentFilters()
	if len(got) != 1 || got[0].Extension != "bots" || got[0].ID != "runs" || got[0].Originators[0] != "codex-bot" {
		t.Fatalf("on by default: %+v", got)
	}
	if got := waitSent(t, node, 1); len(got[0]) != 1 || got[0][0].Originators[0] != "codex-bot" {
		t.Fatalf("handed to the node once installed: %v", got)
	}
	_, out := call(t, mux, "GET", "/api/v1/extensions/bots/-/", master, "")
	cs := out["contributions"].([]any)
	if c := cs[0].(map[string]any); c["via"] != "config" || c["can_disable"] != false || c["enabled"] != true {
		t.Fatalf("its switch is the setting: %v", c)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/bots/-/contributions/agent_filters/runs", master, `{"enabled": false}`); code != 409 {
		t.Fatalf("not switched on its own: %d %v", code, out)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/extensions/bots/-/config", master, `{"config": {"hide_agents": false}}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if got := h.agentFilters(); len(got) != 0 {
		t.Fatalf("off in the settings: %+v", got)
	}
	if got := waitSent(t, node, 2); len(got[1]) != 0 {
		t.Fatalf("the node drops them: %v", got)
	}
	call(t, mux, "PUT", "/api/v1/extensions/bots/-/config", master, `{"config": {"hide_agents": false}}`)
	time.Sleep(50 * time.Millisecond)
	waitSent(t, node, 2) // unchanged: not sent again
	_, out = call(t, mux, "GET", "/api/v1/extensions/bots/-/", master, "")
	if c := out["contributions"].([]any)[0].(map[string]any); c["enabled"] != false {
		t.Fatalf("shown off: %v", c)
	}

	call(t, mux, "PUT", "/api/v1/extensions/bots/-/config", master, `{"config": {"hide_agents": true}}`)
	if len(h.agentFilters()) != 1 {
		t.Fatal("on again")
	}
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/bots/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if got := h.agentFilters(); len(got) != 0 {
		t.Fatalf("the extension off: %+v", got)
	}
	if got := h.agentFilters(); got == nil {
		t.Fatal("an empty list, not nil: it tells the nodes to drop theirs")
	}
}
