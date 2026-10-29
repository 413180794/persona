package node

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
)

// herdr pushes its changes over events.subscribe: pollAgents takes a snapshot as soon as one arrives instead of
// every second. The per-second snapshot stays only while the stream is down, or for TRAEX panes whose state is a
// spinner in the title (a spinner-only title change emits no event); otherwise a slow one catches anything missed.

// slowSnapshot is how often pollAgents still snapshots herdr while events arrive.
const slowSnapshot = 10 * time.Second

// herdrLifecycle are the events that change which sessions there are or how they are named.
var herdrLifecycle = []string{
	"workspace.created", "workspace.renamed", "workspace.closed", "workspace.moved",
	"tab.created", "tab.renamed", "tab.closed", "tab.moved",
	"pane.created", "pane.closed", "pane.exited", "pane.updated", "pane.moved", "pane.agent_detected",
}

type herdrEvents struct {
	live atomic.Bool   // a subscription is streaming
	kick chan struct{} // something changed: snapshot now (buffered 1)

	mu       sync.Mutex
	panes    string             // the pane set subscribed to, sorted and joined
	resub    context.CancelFunc // ends the current subscription so a new one covers the new pane set
	resubbed bool               // the current subscription was ended by resub, not by herdr
}

func newHerdrEvents() *herdrEvents { return &herdrEvents{kick: make(chan struct{}, 1)} }

func (e *herdrEvents) poke() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// subscriptions: the lifecycle events plus agent status for each pane (herdr only sends those per pane).
func subscriptions(panes []string) []herdr.Subscription {
	subs := make([]herdr.Subscription, 0, len(herdrLifecycle)+len(panes))
	for _, t := range herdrLifecycle {
		subs = append(subs, herdr.Subscription{"type": t})
	}
	for _, p := range panes {
		subs = append(subs, herdr.Subscription{"type": "pane.agent_status_changed", "pane_id": p})
	}
	return subs
}

// watchHerdr keeps one subscription open for as long as the node runs, reopening it when herdr closes it (it drops
// subscribers that fall behind; events lost meanwhile are covered by the snapshot taken on reconnect) and when the
// set of panes changes (a connection takes one subscribe call).
func (n *Node) watchHerdr(ctx context.Context) {
	e := n.events
	backoff := time.Second
	for ctx.Err() == nil {
		panes := n.livePanes()
		sctx, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		e.panes, e.resub, e.resubbed = strings.Join(panes, " "), cancel, false
		e.mu.Unlock()
		started := time.Now()
		err := n.herdr.Subscribe(sctx, subscriptions(panes), func(herdr.Event) {
			e.live.Store(true) // the first one is SubscriptionStarted: snapshot to cover the gap before it
			e.poke()
		})
		e.live.Store(false)
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

// livePanes are the panes of the last snapshot's agents, sorted.
func (n *Node) livePanes() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	panes := make([]string, 0, len(n.agents))
	for _, a := range n.agents {
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
	if n.events == nil || !n.events.live.Load() || time.Since(last) >= slowSnapshot {
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
