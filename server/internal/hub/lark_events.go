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
// lark.account is followed from the start; every other signed-in account gets its own connection once the account
// list shows it, and loses it when the account goes (larkAccountEvents).
func (h *Hub) larkEvents(ctx context.Context) {
	go h.larkFollow(ctx, h.lark)
	following := map[int]context.CancelFunc{}
	t := time.NewTicker(larkAccountsPoll)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-time.After(larkAccountsFirst):
	}
	for {
		if !h.isMember() {
			h.larkAccountEvents(ctx, following)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// How soon after start and how often the realtime connections follow the account list (variables for tests).
var larkAccountsFirst, larkAccountsPoll = 5 * time.Second, 2 * time.Minute

// larkAccountEvents starts a connection for each signed-in account that has none and stops those of accounts that
// are gone or signed out.
func (h *Hub) larkAccountEvents(ctx context.Context, following map[int]context.CancelFunc) {
	want := map[int]bool{}
	for _, a := range h.larkSignedIn(ctx)[1:] {
		want[a.ID] = true
	}
	for id, stop := range following {
		if !want[id] {
			stop()
			delete(following, id)
		}
	}
	for id := range want {
		if following[id] == nil {
			actx, stop := context.WithCancel(ctx)
			following[id] = stop
			go h.larkFollow(actx, h.lark.ForAccount(id))
		}
	}
}

// larkFollow relays one account's events. Bursts (a busy group, one message firing message + conversation events)
// go out once per larkCoalesce. /agent requests, personal agents and the knowledge base are lark.account's alone.
func (h *Hub) larkFollow(ctx context.Context, c *lark.Client) {
	const larkCoalesce = 400 * time.Millisecond
	primary := c == h.lark
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
		message["account"] = c.Account()
		if !primary {
			message["ol_account"] = c.Account() // what the apps pass to the /api/v1/lark routes for these chats
		}
		h.broadcast(message)
		h.kickLarkChats()
		if primary {
			h.kbKick()
		}
	}
	logf := h.logf
	if !primary {
		logf = func(format string, args ...any) {
			h.logf("[account %d] "+format, append([]any{c.Account()}, args...)...)
		}
	}
	c.Events(ctx, func(ev lark.Event) {
		if h.isMember() {
			return // the center's own Feishu connection serves the group (mesh_group.go)
		}
		if h.larkByPlugin() {
			// The plugin serving Feishu has its own event stream: it pushes the chats, its connector's inbox and
			// archive, /agent and the personal agents (agentsrc.updated); here they would go out twice.
			return
		}
		h.larkLastEvent.Store(time.Now().Unix())
		if ev.Type == "agent-bridge.updated" { // personal agents: refresh just those sessions, not the Feishu inbox
			if primary && h.agents != nil {
				h.agents.Updated(agents.OLKBridge, ev.SessionKeys)
			}
			return
		}
		if primary {
			h.larkAgentEvent(ev) // a /agent request starts at once, not with the coalesced refresh
		}
		mu.Lock()
		defer mu.Unlock()
		pending.add(ev)
		if timer == nil { // a throttle, not a debounce: a group that never goes quiet still gets through
			timer = time.AfterFunc(larkCoalesce, flush)
		}
	}, logf)
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
