package hub

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// larkEvents relays lark-connector's realtime events to the apps as {"type":"lark","feed_ids":[…]}: the push half of
// keeping Feishu fresh. The apps pull what changed (the conversation list, the latest page of each chat).
// Bursts (a busy group, one message firing message + conversation events) go out once per larkCoalesce.
func (h *Hub) larkEvents(ctx context.Context) {
	const larkCoalesce = 400 * time.Millisecond
	var (
		mu      sync.Mutex
		pending = map[string]bool{} // an event without conversations (settings) still sends an empty list
		timer   *time.Timer
	)
	flush := func() {
		mu.Lock()
		ids := make([]string, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		pending, timer = map[string]bool{}, nil
		mu.Unlock()
		h.broadcast(map[string]any{"type": "lark", "feed_ids": ids})
	}
	h.lark.Events(ctx, func(ev lark.Event) {
		mu.Lock()
		defer mu.Unlock()
		for _, id := range ev.FeedIDs {
			pending[id] = true
		}
		if timer == nil { // a throttle, not a debounce: a group that never goes quiet still gets through
			timer = time.AfterFunc(larkCoalesce, flush)
		}
	}, h.logf)
}
