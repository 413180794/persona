package hub

import (
	"context"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// agentState maps a host status plus Jarvis' own knowledge onto the unified states (docs/agent-states.md):
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
	if h.store != nil {
		if err := h.store.MarkSeen(context.Background(), machine, session, now); err != nil {
			h.logf("mark seen: %v", err)
		}
	}
	if m := h.localMachine(machine); m != nil {
		h.broadcastSessions(m, h.decorated(m))
	}
}
