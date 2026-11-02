package hub

import (
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// A runtime's session (docs/runtimes.md) has no terminal: when it is blocked (waiting in the runtime's own UI) the hub
// does not read a screen and put an empty terminal card on the phone; a session with a terminal still gets one.
func TestNoScreenCardWithoutATerminal(t *testing.T) {
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	f.mu.Lock()
	f.screen = "  Pick a model\n\n› 1. gpt-6\n  2. gpt-6-mini\n\n  Press enter to confirm or esc to go back\n"
	f.mu.Unlock()
	runtime := proto.Session{SessionID: "session-dsh", Status: proto.StatusBlocked, Agent: "dsh", SourceKind: proto.SourceRuntime,
		Capabilities: &proto.SessionCapabilities{Read: true, Send: true, Approve: true}}
	list := []proto.Session{runtime}
	m.mu.Lock()
	m.sessions = list
	m.mu.Unlock()
	h.syncScreenInteractions(m, list)
	time.Sleep(2 * time.Second)
	if got := pendingIDs(h); len(got) != 0 {
		t.Fatalf("a screen card for a session without a terminal: %v", got)
	}

	terminal := proto.Session{SessionID: "s1", Status: proto.StatusBlocked}
	list = []proto.Session{runtime, terminal}
	m.mu.Lock()
	m.sessions = list
	m.mu.Unlock()
	h.syncScreenInteractions(m, list)
	deadline := time.Now().Add(5 * time.Second)
	for len(pendingIDs(h)) < 1 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	got := pendingIDs(h)
	if len(got) != 1 {
		t.Fatalf("want one screen card, for the terminal session: %v", got)
	}
	for id, kind := range got {
		if kind != proto.IxScreen || id == "" {
			t.Fatalf("%s %s", id, kind)
		}
	}
}

// A runtime's card is pushed under its own name.
func TestRuntimePushTitle(t *testing.T) {
	p := interactionPayload(proto.Interaction{Kind: proto.IxPermission, Agent: "dsh", AgentLabel: "DSH", Summary: "echo hi"}, "server")
	alert := p["aps"].(map[string]any)["alert"].(map[string]any)
	if alert["title"] != "DSH 需要你授权" {
		t.Errorf("%v", alert)
	}
}
