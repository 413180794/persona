package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/agents"
	"github.com/nwverse/persona/server/internal/lark"
)

// larkEvents relays lark-connector's realtime events to the apps: the push half of
// keeping Feishu fresh. The apps pull what changed (the conversation list, the latest page of each chat).
// Bursts (a busy group, one message firing message + conversation events) go out once per larkCoalesce.
func (h *Hub) larkEvents(ctx context.Context) {
	const larkCoalesce = 400 * time.Millisecond
	var (
		mu      sync.Mutex
		pending larkEventBatch
		timer   *time.Timer
	)
	flush := func() {
		mu.Lock()
		message := pending.drain()
		timer = nil
		mu.Unlock()
		h.broadcast(message)
		h.kickLarkChats()
		h.kbKick()
	}
	h.lark.Events(ctx, func(ev lark.Event) {
		if h.isMember() {
			return // the center's own Feishu connection serves the group (mesh_group.go)
		}
		h.larkLastEvent.Store(time.Now().Unix())
		if ev.Type == "agent-bridge.updated" { // personal agents: refresh just those sessions, not the Feishu inbox
			if h.agents != nil {
				h.agents.Updated(agents.OLKBridge, ev.SessionKeys)
			}
			return
		}
		h.larkAgentEvent(ev) // a /agent request starts at once, not with the coalesced refresh
		mu.Lock()
		defer mu.Unlock()
		pending.add(ev)
		if timer == nil { // a throttle, not a debounce: a group that never goes quiet still gets through
			timer = time.AfterFunc(larkCoalesce, flush)
		}
	}, h.logf)
}

// Settings and messages can share one throttle window. Keep their invalidations independent.
// The caller holds the batch lock while adding or draining.
type larkEventBatch struct {
	feeds           map[string]bool
	settingsChanged bool
}

func (b *larkEventBatch) add(ev lark.Event) {
	if b.feeds == nil {
		b.feeds = make(map[string]bool)
	}
	for _, id := range ev.FeedIDs {
		b.feeds[id] = true
	}
	b.settingsChanged = b.settingsChanged || ev.Type == "local.settings.updated"
}

func (b *larkEventBatch) drain() map[string]any {
	ids := make([]string, 0, len(b.feeds))
	for id := range b.feeds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := map[string]any{"type": "lark", "feed_ids": ids, "settings_changed": b.settingsChanged}
	*b = larkEventBatch{}
	return out
}
