package node

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
)

func TestCodexLegacyCollisionDoesNotTargetClaudeOrServer(t *testing.T) {
	n := remoteTestNode(t)
	home := n.codexHome()
	f, _ := connectRemoteFixture(t, n, home, "one", remoteRaw)
	n.mu.Lock()
	n.agents[remoteRaw] = host.Agent{Agent: "claude"}
	n.mu.Unlock()
	if !n.codexBlocked(remoteRaw) {
		t.Fatal("cross-product UUID collision not detected")
	}
	for _, method := range []string{"session.send", "session.keys", "session.items", "session.interrupt"} {
		if _, err := n.handle(context.Background(), method, json.RawMessage(`{"session_id":"`+remoteRaw+`","text":"must not execute"}`)); err == nil {
			t.Fatalf("collision allowed %s", method)
		}
	}
	if f.count("thread/resume")+f.count("turn/start") != 0 {
		t.Fatal("ambiguous legacy id dispatched")
	}
	n2 := remoteTestNode(t)
	n2.configureCodex(n.codexInventory())
	if !n2.codexBlocked(remoteRaw) {
		t.Fatal("old outbox could be redirected after restart")
	}
}
