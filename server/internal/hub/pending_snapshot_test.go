package hub

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A card the manager ends while an app is getting the pending cards on connecting must not reach that app after its
// interaction_resolved: the app would show it as waiting for the user until it reconnects.
func TestPendingSnapshotNeverOutlivesItsResolve(t *testing.T) {
	h := newTestHub(t)
	for i := range 300 {
		ix := proto.Interaction{ID: fmt.Sprintf("ix_race%d", i), MachineID: "test-machine", SessionID: "test-session",
			Kind: proto.IxPermission, ToolName: "Bash", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute)}
		reportInteraction(t, h, ix)
		c := &client{out: make(chan []byte, 64)}
		h.mu.Lock()
		h.clients[c] = struct{}{}
		h.mu.Unlock()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); h.sendPendingInteractions(c, false) }()
		go func() { defer wg.Done(); reportResolved(t, h, ix.ID, "") }()
		wg.Wait()
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		last := ""
		for len(c.out) > 0 {
			var e struct {
				Type        string            `json:"type"`
				ID          string            `json:"id"`
				Interaction proto.Interaction `json:"interaction"`
			}
			_ = json.Unmarshal(<-c.out, &e)
			if e.ID == ix.ID || e.Interaction.ID == ix.ID {
				last = e.Type
			}
		}
		if last == "interaction" {
			t.Fatalf("round %d: the app got the card after it was resolved: it stays on screen as waiting", i)
		}
	}
}
