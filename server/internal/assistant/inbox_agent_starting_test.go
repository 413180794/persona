package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// What comes for an event while its conversation is being started is not in the first message (rendered before the
// start): it is still told once the start returns, and a new message that asks for work does not start a second one.
func TestInboxNotesToldWhileTheAgentStartsAreKept(t *testing.T) {
	for _, wake := range []bool{false, true} {
		t.Run(map[bool]string{false: "note", true: "waking note"}[wake], func(t *testing.T) {
			h := agentHub(t)
			it := actionItem("x", time.Now())
			it.Agent = &InboxAgent{State: AgentQueued, Task: "查 1013 是否合入"}
			h.inbox.items[it.ID] = it
			called := false
			hook := func(f *fakeHub, p map[string]any) {
				called = true
				h.inbox.mu.Lock()
				h.inbox.tell(it, inboxNote{Name: prompts.NoteNewMessages}, wake, time.Now())
				h.inbox.mu.Unlock()
			}
			h.f.mu.Lock()
			h.f.onStart, h.f.onAssistantSend = hook, hook
			h.f.mu.Unlock()
			h.startInboxAgents(context.Background(), InboxAgentConfig{Enabled: true})
			h.f.mu.Lock()
			h.f.onStart, h.f.onAssistantSend = nil, nil
			h.f.mu.Unlock()
			if !called {
				t.Fatal("the start never reached the node")
			}
			h.inbox.mu.Lock()
			a := *it.Agent
			h.inbox.mu.Unlock()
			if a.SessionID == "" || a.State != AgentRunning {
				t.Fatalf("wake %v: the started conversation was not kept: %+v", wake, a)
			}
			h.f.mu.Lock()
			told := 0
			for _, p := range h.f.agentSent {
				if p["session_id"] == a.SessionID {
					told++
				}
			}
			h.f.mu.Unlock()
			if told != 1 || len(a.Pending) != 0 {
				t.Fatalf("wake %v: the note told during the start went into the conversation %d times, pending %+v", wake, told, a.Pending)
			}
		})
	}
}
