package node

import (
	"slices"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// Ownership (#2576, docs/integrations/engines.md "Host sets"): with several hosts enabled, the same Claude session can be live in
// two of them at once — resumed in herdr while its Orca pane still runs it, say. A session is listed once, in the host
// that owns it; every send, key and interrupt goes there (they route by the listed pane), its state comes from there,
// and its row names the other hosts it is also open in (proto.Session.AlsoOpenIn) so the apps can warn of the
// conflict. The owner, in order:
//
//  1. the pane whose own claude process runs the session (~/.claude/sessions/<pid>.json, sessionFromProcess);
//  2. else the pane whose SessionStart hook named the session last;
//  3. else the pane in the machine's default host;
//  4. else the first host in proto.EngineHostIDs' order, then the lowest pane id (stable across refreshes).
//
// Two live panes of the same host keep the behaviour from before hosts could be combined (the later one listed).

// ownership is what the rules remember between refreshes.
type ownership struct {
	mu        sync.Mutex
	starts    map[string]paneStart // pane ref → the session its last SessionStart hook named, and when
	alsoOpen  map[string][]string  // session id → the other host kinds it is live in (the last refresh)
	ownerKind map[string]string    // session id → the host kind that owns it (the last refresh), for tests and logs
}

type paneStart struct {
	session string
	at      time.Time
}

// noteSessionStart remembers a SessionStart hook from a pane (rule 2).
func (n *Node) noteSessionStart(pane, id string) {
	if pane == "" || !validID(id) {
		return
	}
	n.owners.mu.Lock()
	defer n.owners.mu.Unlock()
	if n.owners.starts == nil {
		n.owners.starts = map[string]paneStart{}
	}
	n.owners.starts[pane] = paneStart{session: id, at: time.Now()}
}

// startedAt is when pane's last SessionStart hook named session id (zero: it did not, or never said).
func (n *Node) startedAt(pane, id string) time.Time {
	n.owners.mu.Lock()
	defer n.owners.mu.Unlock()
	if st, ok := n.owners.starts[pane]; ok && st.session == id {
		return st.at
	}
	return time.Time{}
}

// contest is the live panes of other hosts that list the same session (setAgentsAt); resolved outside n.mu.
type contest map[string][]host.Agent

// add notes that a and b, both live in different hosts, list session id.
func (c contest) add(id string, a, b host.Agent) {
	for _, x := range []host.Agent{a, b} {
		if !slices.ContainsFunc(c[id], func(y host.Agent) bool { return y.PaneID == x.PaneID }) {
			c[id] = append(c[id], x)
		}
	}
}

// crossHost: a running agent of another host already lists session id in m (the case ownership decides).
func crossHost(m map[string]host.Agent, id string, a host.Agent) bool {
	prev, ok := m[id]
	return ok && prev.AgentStatus != proto.StatusOffline && a.AgentStatus != proto.StatusOffline &&
		host.ParsePane(prev.PaneID).Kind != host.ParsePane(a.PaneID).Kind
}

// resolve settles every contested session in m (the owner listed, the others recorded) and remembers which other
// hosts each is also open in. Called without n.mu: rule 1 asks the panes' processes, every candidate's host even in a
// refresh of another host (unlike the rest of setAgentsAt, #5816): a contest is rare, and its owner must not depend on
// which host refreshed last.
func (n *Node) resolve(m map[string]host.Agent, c contest) map[string][]string {
	also := map[string][]string{}
	owners := map[string]string{}
	for id, cands := range c {
		owner := n.ownerOf(id, cands)
		m[id] = owner
		owners[id] = host.ParsePane(owner.PaneID).Kind
		for _, a := range cands {
			if k := host.ParsePane(a.PaneID).Kind; a.PaneID != owner.PaneID && !slices.Contains(also[id], k) {
				also[id] = append(also[id], k)
			}
		}
		slices.Sort(also[id])
	}
	n.owners.mu.Lock()
	prev := n.owners.ownerKind
	n.owners.alsoOpen, n.owners.ownerKind = also, owners
	n.owners.mu.Unlock()
	for id, k := range owners {
		if prev[id] != k {
			n.logf("session %s: live in %v, owned by %s", id, also[id], k)
		}
	}
	return also
}

// ownerOf picks the owner among the live panes listing session id (the rules above).
func (n *Node) ownerOf(id string, cands []host.Agent) host.Agent {
	// 1. the pane whose own claude runs it
	var byProcess []host.Agent
	for _, a := range cands {
		if n.sessionFromProcess(a.PaneID) == id {
			byProcess = append(byProcess, a)
		}
	}
	if len(byProcess) == 1 {
		return byProcess[0]
	}
	if len(byProcess) > 1 {
		cands = byProcess
	}
	// 2. the last SessionStart hook naming it
	var latest []host.Agent
	var at time.Time
	for _, a := range cands {
		t := n.startedAt(a.PaneID, id)
		switch {
		case t.IsZero():
		case t.After(at):
			latest, at = []host.Agent{a}, t
		case t.Equal(at):
			latest = append(latest, a)
		}
	}
	if len(latest) == 1 {
		return latest[0]
	}
	if len(latest) > 1 {
		cands = latest
	}
	// 3. the default host
	def := n.defaultKind()
	var inDefault []host.Agent
	for _, a := range cands {
		if host.ParsePane(a.PaneID).Kind == def {
			inDefault = append(inDefault, a)
		}
	}
	if len(inDefault) >= 1 {
		cands = inDefault
	}
	// 4. stable order
	return slices.MinFunc(cands, func(a, b host.Agent) int {
		if d := hostRank(a.PaneID) - hostRank(b.PaneID); d != 0 {
			return d
		}
		switch {
		case a.PaneID < b.PaneID:
			return -1
		case a.PaneID > b.PaneID:
			return 1
		}
		return 0
	})
}

// hostRank orders a pane's host by proto.EngineHostIDs; hosts outside it (botmux) last.
func hostRank(pane string) int {
	if i := slices.Index(proto.EngineHostIDs, host.ParsePane(pane).Kind); i >= 0 {
		return i
	}
	return len(proto.EngineHostIDs)
}

// alsoOpenIn are the other hosts session id was live in at the last refresh (its row's AlsoOpenIn).
func (n *Node) alsoOpenIn(id string) []string {
	n.owners.mu.Lock()
	defer n.owners.mu.Unlock()
	return slices.Clone(n.owners.alsoOpen[id])
}
