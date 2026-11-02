package hub

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/screen"
)

// Codex question and approval cards (docs/codex.md「提问和审批」). A node lists the ones open on its machine whenever the
// list changes (codex.interactions, node/codexask.go); the hub keeps a card for each — a new one is announced, one no
// longer listed (answered at the computer, the turn interrupted, the agent gone) ends. An answer goes back to the node
// (codex.answer), which types it into the TUI and says whether Codex took it: that error reaches whoever answered.

// codexAnswerTimeout: the node checks the screen, types, and waits for Codex to take the answer (its codexConfirm).
var codexAnswerTimeout = time.Minute

// codexAnnounced remembers which cards were announced (pushed) lately: a node that reconnects lists them again, and
// the phone should not ring twice for one question.
const codexAnnouncedFor = 24 * time.Hour

func (h *Hub) syncCodexInteractions(m *Machine, params json.RawMessage) {
	var p struct {
		Interactions []proto.Interaction `json:"interactions"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	listed := map[string]proto.Interaction{}
	for _, ix := range p.Interactions {
		ix.MachineID = m.cfg.ID
		listed[ix.ID] = ix
	}
	h.mu.Lock()
	var gone, screens []string
	for id, pd := range h.interactions {
		if !pd.codex || pd.ix.MachineID != m.cfg.ID {
			continue
		}
		if _, ok := listed[id]; ok {
			delete(listed, id) // already a card
		} else {
			gone = append(gone, id)
		}
	}
	var added []proto.Interaction
	for _, ix := range listed {
		if h.interactions[ix.ID] != nil {
			continue
		}
		p := h.codexPending(m, ix)
		// A screen card for the same session read the terminal without knowing Codex: an approval or an overlay
		// question card is that dialog read properly. An async question is not on the terminal's keyboard at all.
		for id, old := range h.interactions {
			if coversScreen(p) && old.ix.Kind == proto.IxScreen && old.ix.MachineID == ix.MachineID && old.ix.SessionID == ix.SessionID {
				delete(h.interactions, id)
				screens = append(screens, id)
			}
		}
		h.interactions[ix.ID] = p
		added = append(added, ix)
	}
	now := time.Now()
	quiet := map[string]bool{}
	if h.codexAnnounced == nil {
		h.codexAnnounced = map[string]time.Time{}
	}
	for id, at := range h.codexAnnounced {
		if now.Sub(at) > codexAnnouncedFor {
			delete(h.codexAnnounced, id)
		}
	}
	for _, ix := range added {
		if _, ok := h.codexAnnounced[ix.ID]; ok {
			quiet[ix.ID] = true
		}
		h.codexAnnounced[ix.ID] = now
	}
	h.mu.Unlock()
	for _, id := range screens {
		h.recordEnd(id, "cancelled", nil)
		h.broadcast(map[string]any{"type": "interaction_resolved", "id": id})
		h.push.clear(id)
	}
	for _, id := range gone {
		h.recordEnd(id, "cancelled", nil)
		h.finishInteraction(id)
	}
	for _, ix := range added {
		h.record(ix)
		if quiet[ix.ID] {
			h.show(ix)
		} else {
			h.announce(ix)
		}
	}
}

// codexPending is the hub's side of a Codex card: answering it asks the node. When the node could not type the answer
// (a draft in the box, the TUI elsewhere) the card comes back, still open on the computer; when the node says Codex
// no longer asks it (prompt_changed), the node's next list has what is there now.
func (h *Hub) codexPending(m *Machine, ix proto.Interaction) *pending {
	p := &pending{ix: ix, codex: true}
	p.local = func(r proto.Resolution) error {
		ctx, cancel := context.WithTimeout(context.Background(), codexAnswerTimeout)
		defer cancel()
		err := m.call(ctx, "codex.answer", map[string]any{"session_id": ix.SessionID, "id": ix.ID, "resolution": r}, nil)
		var re *rpc.Error
		if err != nil && !r.Dismiss && !(errors.As(err, &re) && re.Code == screen.PromptChanged) {
			h.mu.Lock()
			if h.interactions[ix.ID] == nil {
				h.interactions[ix.ID] = p
			}
			h.mu.Unlock()
			h.show(ix)
		}
		return err
	}
	return p
}

// show puts a card on every app without a push (one they were told about already).
func (h *Hub) show(ix proto.Interaction) {
	h.broadcast(map[string]any{"type": "interaction", "interaction": ix})
	if m := h.localMachine(ix.MachineID); m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
}
