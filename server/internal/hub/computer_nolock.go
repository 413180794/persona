package hub

// 「扩展开着时不自动锁屏」 (docs/computer-use.md「锁屏、防睡眠、按 App 批准」b2, #1507): the plugin cua's no_auto_lock,
// per Mac and off by default, asked about before it is turned on (x-confirm). While it is on for a Mac (and the
// plugin and that Mac are), its node holds caffeinate -dims; the hub says so again every noLockEvery, and the node's
// hold runs out by itself 30 minutes after the last word, so a Mac the hub no longer reaches goes back to locking.

import (
	"context"
	"slices"
	"strings"
	"time"
)

const noLockEvery = 10 * time.Minute

// noLockOn: m is to be kept from locking now.
func (h *Hub) noLockOn(m *Machine) bool {
	c := h.computerOf(m)
	return c != nil && c.Enabled && slices.Contains(h.cua().NoAutoLock, m.cfg.ID)
}

// tellNoLock says to m's node whether to keep it from locking now.
func (h *Hub) tellNoLock(ctx context.Context, m *Machine) {
	m.mu.Lock()
	darwin := m.info.OS == "darwin"
	m.mu.Unlock()
	if !darwin || !m.isOnline() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.callBuiltin(ctx, "computer.nolock", map[string]any{"on": h.noLockOn(m)}, nil); err != nil && !strings.Contains(err.Error(), "unknown method") {
		h.logf("[%s] computer.nolock: %v", m.cfg.ID, err)
	}
}

// noLockLoop says it again to the Macs it is on for, every noLockEvery.
func (h *Hub) noLockLoop(ctx context.Context) {
	t := time.NewTicker(noLockEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, m := range h.machinesNow() {
				if h.noLockOn(m) {
					go h.tellNoLock(ctx, m)
				}
			}
		}
	}
}
