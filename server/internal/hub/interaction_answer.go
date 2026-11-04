package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The interaction queue as the assistant's app (app.call interactions): one interaction as it is now, and a question
// answered by the assistant. Its interaction_answer tool is the assistant's, with what may be answered and how
// (docs/assistant.md).

// interactionsCall answers the interaction queue's methods (assistantwire.InteractionsGet, InteractionsAnswer).
func (h *Hub) interactionsCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var a struct {
		MachineID     string            `json:"machine_id"`
		SessionID     string            `json:"session_id"`
		InteractionID string            `json:"interaction_id"`
		Answers       map[string]string `json:"answers"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(a.InteractionID)
	switch method {
	case assistantwire.InteractionsGet:
		h.mu.Lock()
		p := h.interactions[id]
		h.mu.Unlock()
		if p == nil {
			return map[string]any{"ok": false, "status": h.interactionOutcome(ctx, id)}, nil
		}
		return map[string]any{"interaction": p.ix, "ok": true}, nil
	case assistantwire.InteractionsAnswer:
		ok, err := h.answerQuestion(a.MachineID, a.SessionID, id, a.Answers)
		if err != nil {
			return nil, err
		}
		if !ok {
			return map[string]any{"ok": false, "status": h.interactionOutcome(ctx, id)}, nil
		}
		return map[string]any{"ok": true}, nil
	}
	return nil, fmt.Errorf("app %s has no method %s", assistantwire.AppInteractions, method)
}

// answerQuestion resolves the pending question id of machine's session with answers, as the assistant answered it;
// false when it is no longer pending.
func (h *Hub) answerQuestion(machine, session, id string, answers map[string]string) (bool, error) {
	h.mu.Lock()
	p := h.interactions[id]
	var ix proto.Interaction
	if p != nil {
		ix = p.ix
	}
	h.mu.Unlock()
	if p == nil {
		return false, nil
	}
	if ix.Kind != proto.IxQuestion {
		return false, fmt.Errorf("interaction %s is a %s, not a question", id, ix.Kind)
	}
	if ix.MachineID != machine || ix.SessionID != session {
		return false, fmt.Errorf("interaction %s is not machine=%s session_id=%s's", id, machine, session)
	}
	if err := h.resolve(id, proto.Resolution{Answers: answers, AnsweredBy: proto.AnsweredByAssistant}); err != nil {
		if errors.Is(err, errNotPending) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// interactionOutcome is how an interaction that is no longer pending ended, from the history: resolved, dismissed,
// expired, cancelled; "" when the history has none.
func (h *Hub) interactionOutcome(ctx context.Context, id string) string {
	if h.store == nil {
		return ""
	}
	status, _, err := h.store.InteractionOutcome(ctx, id)
	if err != nil {
		return ""
	}
	return status
}
