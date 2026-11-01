package node

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
)

// herdr pushes its changes over events.subscribe: pollAgents takes a snapshot as soon as one arrives instead of
// every second. The per-second snapshot stays only while the stream is down, or for TRAEX panes whose state is a
// spinner in the title (a spinner-only title change emits no event); otherwise a slow one catches anything missed.

// slowSnapshot is how often pollAgents still snapshots herdr while events arrive.
const slowSnapshot = 10 * time.Second

type herdrEvents struct {
	live   atomic.Int64 // the watcher whose subscription is streaming (watcherID), 0: none
	nextID atomic.Int64
	kick   chan struct{} // something changed: snapshot now (buffered 1)

	mu       sync.Mutex
	panes    string             // the pane set subscribed to, sorted and joined
	resub    context.CancelFunc // ends the current subscription so a new one covers the new pane set
	resubbed bool               // the current subscription was ended by resub, not by herdr
}

func newHerdrEvents() *herdrEvents { return &herdrEvents{kick: make(chan struct{}, 1)} }

// watcherID is a new watcher's own id: a watcher that stops clears only its own live state, never the one of the
// watcher that replaced it (an engine switch).
func (e *herdrEvents) watcherID() int64 { return e.nextID.Add(1) }

func (e *herdrEvents) setLive(id int64)   { e.live.Store(id) }
func (e *herdrEvents) clearLive(id int64) { e.live.CompareAndSwap(id, 0) }
func (e *herdrEvents) isLive() bool       { return e.live.Load() != 0 }

func (e *herdrEvents) poke() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// watchHerdr keeps one subscription open for as long as the node runs, reopening it when herdr closes it (it drops
// subscribers that fall behind; events lost meanwhile are covered by the snapshot taken on reconnect) and when the
// set of panes changes (a connection takes one subscribe call). It watches the herdr host (host.Watcher): the only
// host so far, and the one these events stand for (a second host that pushes changes needs its own herdrEvents).
func (n *Node) watchHerdr(ctx context.Context) {
	w, ok := n.hosts.Get(host.Herdr).(host.Watcher)
	if !ok {
		return
	}
	e := n.events
	me := e.watcherID()
	backoff := time.Second
	for ctx.Err() == nil {
		panes := n.livePanes()
		sctx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.panes, e.resub, e.resubbed = strings.Join(panes, " "), cancel, false
		e.mu.Unlock()
		started := time.Now()
		refs := make([]host.PaneRef, len(panes))
		for i, p := range panes {
			refs[i] = n.hosts.Ref(p)
		}
		err := w.Watch(sctx, refs, func() {
			e.setLive(me) // the first one is SubscriptionStarted: snapshot to cover the gap before it
			e.poke()
		})
		e.clearLive(me)
		cancel()
		e.mu.Lock()
		resubbed := e.resubbed
		e.mu.Unlock()
		if resubbed || ctx.Err() != nil {
			continue
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		if err != nil && backoff >= 8*time.Second {
			n.logger.Printf("herdr events: %v (retrying in %s)", err, backoff)
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// livePanes are the herdr panes of the last snapshot's agents, sorted.
func (n *Node) livePanes() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	panes := make([]string, 0, len(n.agents))
	for _, a := range n.agents {
		if n.hosts.Ref(a.PaneID).Kind != host.Herdr {
			continue
		}
		if a.PaneID != "" && !slices.Contains(panes, a.PaneID) {
			panes = append(panes, a.PaneID)
		}
	}
	slices.Sort(panes)
	return panes
}

// panesChanged resubscribes when a snapshot shows agents in panes the subscription does not cover.
func (n *Node) panesChanged() {
	e := n.events
	if e == nil {
		return
	}
	key := strings.Join(n.livePanes(), " ")
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resub != nil && key != e.panes {
		e.panes, e.resubbed = key, true
		e.resub()
	}
}

// snapshotDue: whether pollAgents should ask herdr for a snapshot on this tick.
func (n *Node) snapshotDue(last time.Time) bool {
	if n.events == nil || !n.events.isLive() || time.Since(last) >= slowSnapshot {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, a := range n.agents {
		if a.Agent == traexAgent {
			return true
		}
	}
	return false
}

// watchHost keeps the change stream of an engine other than herdr open (host.Watcher: herdr-core), reopening it with
// a backoff when it ends: each change takes a snapshot at once, and while it is live pollAgents snapshots only every
// slowSnapshot, as with herdr's. Such a stream covers every pane of its host, so it is not reopened when the panes
// change. A host that is not running fails at once and is tried again after the backoff, without logging (its client
// logs).
func (n *Node) watchHost(ctx context.Context, w host.Watcher) {
	e := n.events
	me := e.watcherID()
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_ = w.Watch(ctx, nil, func() {
			e.setLive(me) // the first one is the stream starting: snapshot to cover the gap before it
			e.poke()
		})
		e.clearLive(me)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
