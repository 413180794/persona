package extmgr

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nwverse/persona/server/internal/lcuconfirm"
	"github.com/nwverse/persona/server/internal/proto"
)

// AnswerComputerElicit is the node's computer.elicit: LCU asked something while a computer call was running. The
// card is a permission or a question, the same ones the phone and the web already show for app approval and
// "ask before acting". The answer is the MCP elicitation result. The model cannot approve it: a resolution the
// assistant wrote, or anything that is not an allow on that card, comes back as cancel.
func (h *Manager) AnswerComputerElicit(ctx context.Context, machineID string, params json.RawMessage) (map[string]any, error) {
	m := h.machineByID(machineID)
	if m == nil {
		return map[string]any{"action": "cancel"}, nil
	}
	return h.answerComputerElicit(ctx, m, params)
}

func (h *Manager) answerComputerElicit(ctx context.Context, m *Machine, params json.RawMessage) (map[string]any, error) {
	var p struct {
		Conversation string          `json:"conversation"`
		Params       json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return map[string]any{"action": "cancel"}, nil
	}
	ix := lcuconfirm.Card(p.Params)
	ix.ID = "ix_" + randomHex(6)
	ix.MachineID = m.cfg.ID
	ix.SessionTitle = "Persona"
	ix.CreatedAt = time.Now().UTC()
	ix.ExpiresAt = time.Now().Add(15 * time.Minute).UTC()
	if home := h.sessionHome(p.Conversation); home != "" {
		// On the assistant conversation the card is enabled, and the assistant's own answer tool refuses it.
		ix.MachineID, ix.SessionID = home, p.Conversation
	}
	if ix.Summary != "" {
		ix.Summary = "在「" + m.cfg.Name + "」上：" + ix.Summary
	}
	if ix.Kind == proto.IxQuestion && len(ix.Questions) > 0 {
		ix.Questions[0].Question = ix.Summary
	}
	res, _ := h.waitInteraction(ctx, ix)
	out := lcuconfirm.Answer(p.Params, res)
	action, _ := out["action"].(string)
	h.logf("[%s] computer elicitation %s → %s", m.cfg.ID, lcuconfirm.Kind(p.Params), action)
	return out, nil
}

// sessionHome is the machine that lists this conversation, so a card for it shows in that chat.
func (h *Manager) sessionHome(id string) string {
	if id == "" {
		return ""
	}
	for _, m := range h.machinesNow() {
		m.mu.Lock()
		found := false
		for _, s := range m.sessions {
			if s.SessionID == id {
				found = true
				break
			}
		}
		mid := m.cfg.ID
		m.mu.Unlock()
		if found {
			return mid
		}
	}
	return ""
}
