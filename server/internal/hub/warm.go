package hub

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Warm start (docs/hub-restart.md「机器和会话」): a hub that stops for a restart leaves the next one what its
// machines looked like (Postgres settings "hub.warm_machines", taken once). For a while after it starts, a machine
// that was online shows as it was, its sessions included, while its node connects again (the hub's own in well under
// a second, an ssh one in a few seconds): the apps that reconnect meanwhile see nothing go offline and come back, and
// a request for it waits for the node instead of failing.

// reconnectGrace: a running hub shows a machine whose node just disconnected as it was this long, while it reconnects
// (a resident node restarting on a new binary takes about 2 s); a variable for tests.
var reconnectGrace = 5 * time.Second

const (
	warmKey   = "hub.warm_machines"
	warmGrace = 30 * time.Second // shown as before at most this long without its node
	warmStale = 2 * time.Minute  // a hand-over older than this is not used (the hub was down, not restarting)
)

type warmMachine struct {
	Info     proto.NodeInfo  `json:"info"`
	Sessions []proto.Session `json:"sessions"`
}

type savedWarm struct {
	Machines map[string]warmMachine `json:"machines"`
	SavedAt  time.Time              `json:"saved_at"`
}

// saveWarm hands the online machines to the next hub (beginStop, before their connections close).
func (h *Hub) saveWarm(ctx context.Context) {
	if h.store == nil {
		return
	}
	saved := savedWarm{Machines: map[string]warmMachine{}, SavedAt: time.Now()}
	for _, m := range h.machinesNow() {
		m.mu.Lock()
		if m.online && !m.derived {
			saved.Machines[m.cfg.ID] = warmMachine{Info: m.info, Sessions: append([]proto.Session(nil), m.sessions...)}
		}
		m.mu.Unlock()
	}
	if err := h.store.PutSetting(ctx, warmKey, saved); err != nil {
		h.logf("warm start: hand over machines: %v", err)
	}
}

// loadWarm takes over what the last hub handed over, before the machines start connecting.
func (h *Hub) loadWarm(ctx context.Context) {
	if h.store == nil {
		return
	}
	var saved savedWarm
	ok, err := h.store.TakeSetting(ctx, warmKey, &saved)
	if err != nil {
		h.logf("warm start: %v", err)
		return
	}
	if !ok || time.Since(saved.SavedAt) > warmStale {
		return
	}
	until := time.Now().Add(warmGrace)
	n := 0
	for id, w := range saved.Machines {
		m := h.byID[id]
		if m == nil {
			continue
		}
		m.mu.Lock()
		if m.online {
			m.mu.Unlock()
			continue
		}
		ch := make(chan struct{})
		m.info, m.sessions, m.warmUntil, m.warmed = w.Info, w.Sessions, until, ch
		n++
		m.mu.Unlock()
		time.AfterFunc(warmGrace, func() { m.warmExpired(ch) })
	}
	if n > 0 {
		h.logf("warm start: %d machines shown as before while their nodes reconnect", n)
	}
}

// warmLocked: shown as before the restart, its node not back yet; m.mu is held.
func (m *Machine) warmLocked(now time.Time) bool { return !m.online && now.Before(m.warmUntil) }

// endWarmLocked: its node is back (or the time is up); m.mu is held.
func (m *Machine) endWarmLocked() {
	m.warmUntil = time.Time{}
	if m.warmed != nil {
		close(m.warmed)
		m.warmed = nil
	}
}

// warmExpired: the node did not come back in time (ch: the wait that ended); the machine shows offline as it would
// have from the start.
func (m *Machine) warmExpired(ch chan struct{}) {
	m.mu.Lock()
	if m.warmed != ch || m.online {
		m.mu.Unlock()
		return
	}
	m.endWarmLocked()
	m.via, m.path = "", ""
	for i := range m.sessions {
		if m.sessions[i].Status != proto.StatusOffline {
			m.sessions[i].Status = proto.StatusUnknown
		}
	}
	why := m.err
	m.mu.Unlock()
	m.hub.logf("[%s] offline: its node did not come back in time", m.cfg.ID)
	if why != "" {
		m.tellLinks(false, why)
	}
	m.hub.broadcastMachines()
	m.hub.sessionsChanged(m)
}

// waitWarm waits, for a call made while the machine is shown as before the restart, until its node is back (true)
// or that ends without it (false).
func (m *Machine) waitWarm(ctx context.Context) bool {
	m.mu.Lock()
	ch := m.warmed
	warm := m.warmLocked(time.Now())
	m.mu.Unlock()
	if !warm || ch == nil {
		return false
	}
	select {
	case <-ch:
	case <-ctx.Done():
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peer != nil
}
