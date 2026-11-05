package hub

import (
	"context"

	"github.com/nwverse/persona/server/internal/proto"
)

// 本会话总是允许 on an assistant conversation (#2202, docs/assistant.md「本会话总是允许」). Claude's session rule and
// Codex's acceptForSession live in the agent's process, and the assistant's process is replaced all the time (idle,
// redeploys, a launcher switch): the resumed one asked about the same tool again. The node names what such a card
// grants (proto.Interaction.AlwaysKeys, only on assistant conversations); the hub keeps the keys once a card is
// answered so, and answers a later card whose keys are all kept itself, with always again so the new process takes
// its own session rule from it.

// maxAlwaysKeys bounds what one card may name (a file change's paths, a gated command's calls).
const maxAlwaysKeys = 32

func alwaysCard(ix proto.Interaction) bool {
	return ix.Kind == proto.IxPermission && ix.SessionID != "" && len(ix.AlwaysKeys) > 0 &&
		len(ix.AlwaysKeys) <= maxAlwaysKeys && len(ix.Fields) == 0
}

// nodeInteraction is a node's interaction.create: allowed right away when 本会话总是允许 already covers it, else
// shown and waited for.
func (h *Hub) nodeInteraction(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
	if r, ok := h.alwaysAllowed(ctx, ix); ok {
		if ix.Resumable { // its hook's earlier attempt left the card pending: it is answered too
			_ = h.resolve(ix.ID, r)
		}
		return r, nil
	}
	return h.waitInteraction(ctx, ix)
}

// alwaysAllowed: every key ix names was granted with 本会话总是允许 in its conversation.
func (h *Hub) alwaysAllowed(ctx context.Context, ix proto.Interaction) (proto.Resolution, bool) {
	if h.store == nil || !alwaysCard(ix) {
		return proto.Resolution{}, false
	}
	ok, err := h.store.AlwaysAllowed(ctx, ix.SessionID, ix.AlwaysKeys)
	if err != nil {
		h.logf("[%s] 本会话总是允许: %v", ix.MachineID, err)
		return proto.Resolution{}, false
	}
	if !ok {
		return proto.Resolution{}, false
	}
	h.logf("[%s] %s allowed: 本会话总是允许 in this conversation", ix.MachineID, ix.ToolName)
	yes := true
	return proto.Resolution{Allow: &yes, Always: true}, true
}

// keepAlways keeps what a card answered with 本会话总是允许 grants, then answers the conversation's other cards
// waiting meanwhile that it covers now (parallel tool calls ask at once).
func (h *Hub) keepAlways(ix proto.Interaction, r proto.Resolution) {
	if h.store == nil || !alwaysCard(ix) || r.Allow == nil || !*r.Allow || !r.Always {
		return
	}
	ctx := context.Background()
	if err := h.store.AllowAlways(ctx, ix.SessionID, ix.ToolName, ix.AlwaysKeys); err != nil {
		h.logf("[%s] keep 本会话总是允许: %v", ix.MachineID, err)
		return
	}
	h.mu.Lock()
	var waiting []proto.Interaction
	for _, p := range h.interactions {
		if p.ix.ID != ix.ID && p.ix.SessionID == ix.SessionID && p.ix.MachineID == ix.MachineID &&
			p.local == nil && !p.codex && !p.claimed && alwaysCard(p.ix) {
			waiting = append(waiting, p.ix)
		}
	}
	h.mu.Unlock()
	for _, w := range waiting {
		if res, ok := h.alwaysAllowed(ctx, w); ok {
			_ = h.resolve(w.ID, res)
		}
	}
}
