package hub

import (
	"context"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// agentState maps a host status plus Persona' own knowledge onto the unified states (docs/agent-states.md):
//
//	herdr blocked, or a pending question / plan / permission  → needs_you (red)
//	herdr working                                              → working
//	herdr done (finished, unseen in herdr) and not seen in the app since it finished → unread (green)
//	herdr idle (seen in herdr), or done but seen in the app    → idle (gray)
func agentState(s proto.Session, seenAt time.Time) string {
	if s.PendingInteractions > 0 || s.Status == proto.StatusBlocked {
		return proto.StateNeedsYou
	}
	switch s.Status {
	case proto.StatusWorking:
		return proto.StateWorking
	case proto.StatusDone:
		// The transcript stops changing when the turn ends, so updated_at is when it finished.
		if !seenAt.IsZero() && !seenAt.Before(s.UpdatedAt.Add(-2*time.Second)) {
			return proto.StateIdle
		}
		return proto.StateUnread
	case proto.StatusIdle:
		return proto.StateIdle
	case proto.StatusOffline:
		return proto.StateOffline
	}
	return proto.StateUnknown
}

// turnError is s's last words when they are a model API error and it has stopped (unread or idle): its latest turn
// was cut off by the model service, not finished (#1516). Only an answer that starts with the error counts: a reply
// that talks about one ("修好了 API Error: … 时的崩溃") is a reply.
func turnError(s proto.Session) string {
	if s.State != proto.StateUnread && s.State != proto.StateIdle {
		return ""
	}
	if text := strings.TrimSpace(s.LastText); cutOff(text) {
		return text
	}
	return ""
}

// cutOff: an agent's answer is the model API error that ended its turn.
func cutOff(answer string) bool { return assistantwire.CutOff(answer) }

// interruptedSummary is what a task or a push says of a turn the model service cut off (#1516).
const interruptedSummary = assistantwire.InterruptedSummary

type stateMark struct {
	state string
	at    time.Time
}

// markStates fills in StateSince: the time an agent's state last changed, kept until it changes again.
func (h *Hub) markStates(machine string, list []proto.Session, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stateSince == nil {
		h.stateSince = map[[2]string]stateMark{}
	}
	for i := range list {
		k := [2]string{machine, list[i].SessionID}
		mk, ok := h.stateSince[k]
		if !ok || mk.state != list[i].State {
			mk = stateMark{state: list[i].State, at: now}
			h.stateSince[k] = mk
		}
		list[i].StateSince = mk.at
	}
}

func (h *Hub) loadSeen(ctx context.Context) {
	if h.store == nil {
		return
	}
	m, err := h.store.SeenTimes(ctx)
	if err != nil {
		h.logf("load seen times: %v", err)
		return
	}
	h.mu.Lock()
	h.seen = m
	h.mu.Unlock()
}

func (h *Hub) seenAt(machine, session string) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seen[[2]string{machine, session}]
}

// markSeen records that the user looked at a session (opened it, or it finished while open) and refreshes states.
func (h *Hub) markSeen(machine, session string) {
	now := time.Now().UTC()
	h.mu.Lock()
	if h.seen == nil {
		h.seen = map[[2]string]time.Time{}
	}
	h.seen[[2]string{machine, session}] = now
	h.mu.Unlock()
	h.workSeen(machine, session, now)
	if h.store != nil {
		if err := h.store.MarkSeen(context.Background(), machine, session, now); err != nil {
			h.logf("mark seen: %v", err)
		}
	}
	if m := h.localMachine(machine); m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
}
