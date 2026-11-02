package hub

import (
	"net/http"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func (h *Hub) hasElicitationClient() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.has("elicitation") {
			return true
		}
	}
	return false
}

// requestHasElicitation binds REST opt-in to an authenticated connection's hello.
// Client headers alone never declare support, and legacy requests have no platform.
func (h *Hub) requestHasElicitation(r *http.Request) bool {
	who, rc := principalOf(r.Context()), requestClientOf(r.Context())
	if rc.Platform == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.mu.Lock()
		matches := c.device == who.ID && c.meta.Platform == rc.Platform && c.meta.Version == rc.Version
		c.mu.Unlock()
		if matches && c.has("elicitation") {
			return true
		}
	}
	return false
}

func visibleInteractions(ixs []proto.Interaction, elicitation bool) []proto.Interaction {
	out := make([]proto.Interaction, 0, len(ixs))
	for _, ix := range ixs {
		if ix.Kind != proto.IxElicitation || elicitation {
			out = append(out, ix)
		}
	}
	return out
}

func (h *Hub) sendPendingInteractions(c *client, elicitationOnly bool) {
	for _, ix := range h.pendingInteractions() {
		if ix.Kind == proto.IxElicitation {
			if !c.has("elicitation") {
				continue
			}
		} else if elicitationOnly {
			continue
		}
		c.sendJSON(map[string]any{"type": "interaction", "interaction": ix})
	}
}
