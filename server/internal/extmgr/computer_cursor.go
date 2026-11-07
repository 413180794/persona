package extmgr

// "Show the agent's cursor" (docs/extensions/computer-use.md "d) Small settings", #1507): the plugin cua's show_cursor, on by
// default. Off, the Macs the plugin is on for run Cua Driver's daemon without its cursor overlay (the node restarts it
// with --no-overlay, node/cua_extras.go); every other Mac is told on, which only undoes an off this hub had said.

import (
	"context"
	"strings"
	"time"
)

// cursorOn: Cua Driver on m is to draw the agent's cursor.
func (h *Manager) cursorOn(m *Machine) bool {
	c := h.computerOf(m)
	return c == nil || !c.Enabled || h.cua().cursor()
}

// tellCursor says to m's node whether Cua Driver draws the agent's cursor. A restart of the daemon takes a few
// seconds, hence the longer wait.
func (h *Manager) tellCursor(ctx context.Context, m *Machine) {
	m.mu.Lock()
	darwin := m.info.OS == "darwin"
	m.mu.Unlock()
	if !darwin || !m.isOnline() {
		return
	}
	m.cursorMu.Lock() // the setting read and said under it, so the last one said is the newest (Machine.cursorMu)
	defer m.cursorMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var out struct {
		Restarted bool `json:"restarted"`
	}
	on := h.cursorOn(m)
	if err := m.callBuiltin(ctx, "computer.cursor", map[string]any{"on": on}, &out); err != nil {
		if !strings.Contains(err.Error(), "unknown method") {
			h.logf("[%s] computer.cursor: %v", m.cfg.ID, err)
		}
		return
	}
	if out.Restarted {
		h.logf("[%s] Cua Driver restarted with the agent cursor %s", m.cfg.ID, map[bool]string{true: "on", false: "off"}[on])
	}
}
