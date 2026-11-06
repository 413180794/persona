package node

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
)

// herdr pushes its changes over events.subscribe: pollAgents takes a snapshot as soon as one arrives instead of
// every second. The per-second snapshot stays only while the stream is down, or for a compatible CLI panes whose state is a
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
	urgent atomic.Bool   // the change is a card's (an agent started or stopped waiting for the user): no pacing
}

func newWatcherSignals() *watcherSignals { return &watcherSignals{kick: make(chan struct{}, 1)} }

// watcherID is a new watcher's own id: a watcher that stops clears only its own live state, never the one of the
// watcher that replaced it (an engine switch).
func (s *watcherSignals) watcherID() int64 { return s.nextID.Add(1) }

func (s *watcherSignals) setLive(id int64)   { s.live.Store(id) }
func (s *watcherSignals) clearLive(id int64) { s.live.CompareAndSwap(id, 0) }
func (s *watcherSignals) isLive() bool       { return s.live.Load() != 0 }

// poke announces a change; urgent: a card comes or goes (Watcher.Watch).
func (s *watcherSignals) poke(urgent bool) {
	if urgent {
		s.urgent.Store(true)
	}
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
	var logs herdr.LogBackoff // a stream that stays down (no herdr on this computer) is reported more and more rarely
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
		err := w.Watch(sctx, refs, func(urgent bool) {
			e.setLive(me) // the first one is SubscriptionStarted: snapshot to cover the gap before it
			e.poke(urgent)
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
			logs.Reset()
		}
		if err != nil && backoff >= 8*time.Second && logs.Due(time.Now()) {
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
// slowSnapshot, or runs a compatible CLI pane whose spinner title emits no event. Kicked and last belong to this host only, so
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

// hostHasTraex reports whether the host's last snapshot holds a live a compatible CLI agent (its state is a spinner in the
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
		_ = w.Watch(ctx, nil, func(urgent bool) {
			sig.setLive(me) // the first one is the stream starting: snapshot to cover the gap before it
			sig.poke(urgent)
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

// waits remembers, per pane, whether the host last said its agent was blocked, to tell a card's status change (into
// or out of blocked) from the others. A pane it has not heard of yet counts as changed. One Watch call's, not shared.
type waits map[string]bool

func (w *waits) changed(pane string, blocked bool) bool {
	if *w == nil {
		*w = waits{}
	}
	was, known := (*w)[pane]
	(*w)[pane] = blocked
	return !known || was != blocked
}

// listEvery: the list is rebuilt at most this often on the hosts' events however busy the agents are, except for a
// card's change (watcherSignals.urgent).
var listEvery = time.Second

// settle: how long a burst of events (a new tab sends several) is let arrive before the one snapshot it takes.
const settle = 50 * time.Millisecond

// nextChange waits for pollAgents' next turn: a tick, or a change a host's stream announced. It returns the hosts that
// announced one (none on a plain tick), after pacing them (settle, and listEvery since the last rebuild unless the
// change is urgent), and false once ctx ends.
func nextChange(ctx context.Context, tick <-chan time.Time, sigs map[string]*watcherSignals, listed time.Time) (map[string]bool, bool) {
	kinds := make([]string, 0, len(sigs))
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())},
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(tick)},
	}
	for kind, sig := range sigs {
		kinds = append(kinds, kind)
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(sig.kick)})
	}
	chosen, _, _ := reflect.Select(cases)
	if chosen == 0 {
		return nil, false
	}
	kicked := map[string]bool{}
	if chosen > 1 {
		// The select took this host's marker: it is kicked though it has none left to drain (#751: since #611 a
		// lone event, an agent turning blocked, waited for the slow snapshot, up to 10 s).
		kicked[kinds[chosen-2]] = true
		pace(sigs, listed)
	}
	// What came meanwhile goes into the same snapshot.
	for kind, sig := range sigs {
		if sig.drained() {
			kicked[kind] = true
		}
	}
	for kind := range kicked {
		sigs[kind].urgent.Store(false)
	}
	return kicked, true
}

// pace waits settle, then until listEvery after the last rebuild unless a host's change is urgent (a card's).
func pace(sigs map[string]*watcherSignals, listed time.Time) {
	start := time.Now()
	until := listed.Add(listEvery)
	if until.Before(start.Add(settle)) {
		until = start.Add(settle)
	}
	for {
		now := time.Now()
		if !now.Before(until) || now.Sub(start) >= settle && anyUrgent(sigs) {
			return
		}
		time.Sleep(min(until.Sub(now), settle/2))
	}
}

func anyUrgent(sigs map[string]*watcherSignals) bool {
	for _, sig := range sigs {
		if sig.urgent.Load() {
			return true
		}
	}
	return false
}
