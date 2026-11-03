package node

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

// herdr pushes its changes over events.subscribe: pollAgents takes a snapshot as soon as one arrives instead of
// every second. The per-second snapshot stays only while the stream is down, or for TRAEX panes whose state is a
// spinner in the title (a spinner-only title change emits no event); otherwise a slow one catches anything missed.
//
// Every watcher host gets its own signals: in the combined engine a herdr-core event refreshes herdr-core only, never
// herdr, and a dead stream of one does not make the other's ticks cheap.

// slowSnapshot is how often pollAgents still snapshots a host while its events arrive.
const slowSnapshot = 10 * time.Second

// watcherSignals is one watcher host's stream liveness and its change channel (buffered 1).
type watcherSignals struct {
	live   atomic.Int64 // the watcher whose subscription is streaming (watcherID), 0: none
	nextID atomic.Int64
	kick   chan struct{} // something changed: snapshot this host now (buffered 1)
}

func newWatcherSignals() *watcherSignals { return &watcherSignals{kick: make(chan struct{}, 1)} }

// watcherID is a new watcher's own id: a watcher that stops clears only its own live state, never the one of the
// watcher that replaced it (an engine switch).
func (s *watcherSignals) watcherID() int64 { return s.nextID.Add(1) }

func (s *watcherSignals) setLive(id int64)   { s.live.Store(id) }
func (s *watcherSignals) clearLive(id int64) { s.live.CompareAndSwap(id, 0) }
func (s *watcherSignals) isLive() bool       { return s.live.Load() != 0 }

func (s *watcherSignals) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// drained reports whether the host's stream announced a change since the last tick, clearing the marker either way.
func (s *watcherSignals) drained() bool {
	select {
	case <-s.kick:
		return true
	default:
		return false
	}
}

// herdrEvents is the herdr watcher's signals plus the state of its pane-aware subscription.
type herdrEvents struct {
	*watcherSignals

	mu       sync.Mutex
	panes    string             // the pane set subscribed to, sorted and joined
	resub    context.CancelFunc // ends the current subscription so a new one covers the new pane set
	resubbed bool               // the current subscription was ended by resub, not by herdr
}

func newHerdrEvents() *herdrEvents {
	return &herdrEvents{watcherSignals: newWatcherSignals()}
}

// watchHerdr keeps one subscription open for as long as the node runs, reopening it when herdr closes it (it drops
// subscribers that fall behind; events lost meanwhile are covered by the snapshot taken on reconnect) and when the
// set of panes changes (a connection takes one subscribe call). Only a machine whose engine registers herdr has it.
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

// hostDue says whether the host needs a snapshot on this tick: always for a host without an event stream (Orca polls
// every second); for a watcher host when its stream announced a change, is not live, has not been snapshotted for
// slowSnapshot, or runs a TRAEX pane whose spinner title emits no event. Kicked and last belong to this host only, so
// one host's events never schedule the other host's snapshot.
func (n *Node) hostDue(kind string, last time.Time, kicked bool) bool {
	sig, ok := n.watcherSignal(kind)
	if !ok {
		return true
	}
	if kicked || !sig.isLive() || time.Since(last) >= slowSnapshot {
		return true
	}
	return n.hostHasTraex(kind)
}

// hostHasTraex reports whether the host's last snapshot holds a live TRAEX agent (its state is a spinner in the
// title, which emits no event).
func (n *Node) hostHasTraex(kind string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, a := range n.agents {
		if a.Agent == traexAgent && n.hosts.Ref(a.PaneID).Kind == kind {
			return true
		}
	}
	return false
}

// resetSignals creates one change signal per registered watcher host, reusing the herdr event state (it also holds
// herdr's subscription bookkeeping). It runs with n.eng.mu held (and once at construction, before anything serves).
func (n *Node) resetSignals() {
	if n.events == nil { // tests construct bare Nodes; production's New always sets it
		n.events = newHerdrEvents()
	}
	sigs := map[string]*watcherSignals{}
	for _, h := range n.hosts.All() {
		if _, ok := h.(host.Watcher); !ok {
			continue
		}
		if h.Kind() == host.Herdr {
			sigs[host.Herdr] = n.events.watcherSignals
		} else {
			sigs[h.Kind()] = newWatcherSignals()
		}
	}
	n.hostSig = sigs
}

// watcherSignal is the registered host's change signal, false when it has none (no Watcher or not registered).
func (n *Node) watcherSignal(kind string) (*watcherSignals, bool) {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	sig, ok := n.hostSig[kind]
	return sig, ok
}

// signalSnapshot is a copy of the watcher signals for one poll tick: an engine switch swaps the map underneath.
func (n *Node) signalSnapshot() map[string]*watcherSignals {
	n.eng.mu.Lock()
	defer n.eng.mu.Unlock()
	out := make(map[string]*watcherSignals, len(n.hostSig))
	for k, v := range n.hostSig {
		out[k] = v
	}
	return out
}

// watchHost keeps the change stream of a non-herdr watcher open (herdr-core today), reopening it with a backoff when
// it ends: each change takes a snapshot of THAT host at once, and while it is live pollAgents snapshots it only every
// slowSnapshot, as with herdr's. Such a stream covers every pane of its host, so it is not reopened when the panes
// change. A host that is not running fails at once and is tried again after the backoff, without logging (its client
// logs).
func (n *Node) watchHost(ctx context.Context, w host.Watcher, sig *watcherSignals) {
	me := sig.watcherID()
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_ = w.Watch(ctx, nil, func() {
			sig.setLive(me) // the first one is the stream starting: snapshot to cover the gap before it
			sig.poke()
		})
		sig.clearLive(me)
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
