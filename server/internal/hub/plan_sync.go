package hub

import (
	"context"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// A plan answered in the terminal can outlive its PermissionRequest hook. Reconcile the pending card with the
// completed ExitPlanMode call, not the agent's status or words: working is not proof that anyone approved a plan.
func (h *Hub) syncPlanInteractions(m *Machine) {
	now := time.Now()
	h.mu.Lock()
	var check []*pending
	for _, p := range h.interactions {
		if p.ix.MachineID != m.cfg.ID || p.ix.Kind != proto.IxPlan || p.ix.ToolName != "ExitPlanMode" || p.local != nil ||
			p.checking || now.Sub(p.checkedAt) < 2*time.Second {
			continue
		}
		p.checking, p.checkedAt = true, now
		check = append(check, p)
	}
	h.mu.Unlock()
	for _, p := range check {
		go h.checkPlanInteraction(m, p)
	}
}

func (h *Hub) checkPlanInteraction(m *Machine, p *pending) {
	defer func() {
		h.mu.Lock()
		p.checking = false
		h.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var items []proto.Item
	before := 0
	// No transcript-wide scan: most plans are in the latest two turns. A few older pages cover a user continuing
	// after answering in the terminal; pages have the node's item and parse budgets too.
	for page := 0; page < 3; page++ {
		var res struct {
			Items   []proto.Item `json:"items"`
			HasMore bool         `json:"has_more"`
		}
		if err := m.call(ctx, "session.items", map[string]any{"session_id": p.ix.SessionID, "turns": 2,
			"before_seq": before, "lazy_images": true}, &res); err != nil || len(res.Items) == 0 {
			return
		}
		items = append(items, res.Items...)
		if planCallFinished(p.ix, items) {
			h.finishExternalPlan(p)
			return
		}
		next := res.Items[0].Seq
		if !res.HasMore || next <= 0 || (before > 0 && next >= before) {
			return
		}
		before = next
	}
}

// PermissionRequest in some Claude Code versions omits tool_use_id. In that case the call immediately preceding
// this hook is identified by its timestamp; ambiguous calls, missing timestamps and earlier results fail closed.
// The result means only that the call ended (approval, rejection or interruption); no resolution is invented.
func planCallFinished(ix proto.Interaction, items []proto.Item) bool {
	id := ix.ToolUseID
	if id == "" {
		if ix.CreatedAt.IsZero() {
			return false
		}
		for _, it := range items {
			if it.Role != "assistant" || it.Kind != proto.KindToolUse || it.ToolName != "ExitPlanMode" || it.ToolUseID == "" ||
				it.TS.IsZero() || it.TS.After(ix.CreatedAt) || ix.CreatedAt.Sub(it.TS) > 30*time.Second {
				continue
			}
			if id != "" && id != it.ToolUseID {
				return false // two candidate plans: neither can be safely attributed to this hook
			}
			id = it.ToolUseID
		}
	}
	if id == "" {
		return false
	}
	for _, it := range items {
		if it.Role == "user" && it.Kind == proto.KindToolResult && it.ToolUseID == id &&
			!it.TS.IsZero() && !it.TS.Before(ix.CreatedAt) {
			return true
		}
	}
	return false
}

func (h *Hub) finishExternalPlan(p *pending) {
	h.mu.Lock()
	if h.interactions[p.ix.ID] != p {
		h.mu.Unlock()
		return // the phone answered or this card was replaced while the read was in flight
	}
	delete(h.interactions, p.ix.ID)
	if p.orphaned != nil {
		p.orphaned.Stop()
	}
	h.mu.Unlock()
	h.recordEnd(p.ix.ID, "dismissed", nil)
	select {
	case p.done <- proto.Resolution{Dismiss: true}:
	default:
	}
	h.finished(p)
}
