package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/streams"
)

var errOffline = apierr.ErrOffline

// Machine is what the hub knows of one of the user's computers: what the manager that carries it reports
// (devices.state, devicewire; hub/devices_link.go) and the node's events it passes on, with what is the hub's own
// beside it — the apps watching its sessions, the Cua / ego state, what the hub shows of its sessions. The hub does not
// connect it: the manager does (internal/devices), and a call goes there over the bus (busNode). Tests attach an
// in-process node as peer instead.
type Machine struct {
	cfg MachineConfig
	hub *Hub

	mu sync.Mutex
	// peer is how calls reach the node: the manager over the bus (busNode) for a machine it reported, or a test's
	// rpc.Peer.
	peer    nodeCaller
	streams *streams.Mux // the forwarded ports' connections on the node's current connection (ports.go)
	info    proto.NodeInfo
	online  bool
	// connectedAt: when the node's current connection began, as the manager says (sessions.changed carries it to the
	// assistant).
	connectedAt time.Time
	// engine is the engine the manager handed over at its last stop, shown until it reports (snap has it then).
	engine string
	// warmUntil: shown as before a restart until then — the hub's, while the manager comes back (calls wait for it in
	// busNode), or the manager's, while the node comes back (its report's) (devices_link.go).
	warmUntil time.Time
	err       string
	lastSeen  time.Time
	sessions  []proto.Session
	watchers  map[string]int // session id -> subscribed clients
	// What the node was told about each session's watch (syncWatch): watchMu orders the session.watch /
	// session.unwatch calls per session (a 1-slot channel, so a wait for it can end with the caller's ctx), nodeWatch
	// is what the last of them left (absent: not watched).
	watchMu   map[string]chan struct{}
	nodeWatch map[string]nodeWatchState
	// The web panels' open folders on this machine (fswatch.go): fswMu orders the fs.watch calls, fswSent says the
	// node was given a non-empty set, fswWarned that a failure was logged (a node without fs.watch).
	fswMu     sync.Mutex
	fswSent   bool
	fswWarned bool
	lastSeq   map[string]int // session id -> highest item seq seen
	// derived: a computer only a member hub has in hub.json; the center reaches it through that hub.
	derived bool

	// The manager's report (devices_link.go): managed is set once one came for this machine (or the hub made it for
	// one the manager will report); snap is the machine as it said; connected, a call would reach the node now;
	// hookedAt, the connection the hub did its part for (devices.connected); paths, its ways for the admin page.
	managed bool
	// owner is the manager that reported it (ownership.go); "" before one did.
	owner     string
	snap      *proto.Machine
	connected bool
	hookedAt  time.Time
	paths     []pathInfo
}

// nodeCaller is how calls reach a node: *rpc.Peer (tests), busNode (the manager).
type nodeCaller interface {
	Call(ctx context.Context, method string, params, out any) error
	Notify(event string, params any) error
	Done() <-chan struct{}
	Close(err error)
}

// ownerID is the manager that reported m; "" before one did.
func (m *Machine) ownerID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.owner
}

// ownerOrCarrier is m's owner, the carrier of hub.json's machines before one reported it; m.mu must not be held.
func (m *Machine) ownerOrCarrier() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cmp.Or(m.owner, devicesOwner)
}

func newMachine(h *Hub, cfg MachineConfig) *Machine {
	return &Machine{cfg: cfg, hub: h, watchers: map[string]int{}, lastSeq: map[string]int{}}
}

func (m *Machine) snapshot() proto.Machine {
	computer := m.hub.computerOf(m) // before m.mu: it takes it
	browser := m.hub.browserOf(m)
	m.mu.Lock()
	defer m.mu.Unlock()
	warm := m.warmLocked(time.Now())
	var s proto.Machine
	if m.snap != nil {
		s = *m.snap
	} else {
		// Not reported yet (the hub just started, or a test's machine): what the hub has.
		icon := m.cfg.Icon
		if icon == "" {
			icon = "laptop"
		}
		s = proto.Machine{ID: m.cfg.ID, Name: m.cfg.Name, Kind: m.cfg.Kind, Icon: icon, SSHHost: m.cfg.SSHHost,
			Hostname: m.info.Hostname, OS: m.info.OS, Arch: m.info.Arch, Home: m.info.Home, HerdrVersion: m.info.HerdrVersion,
			ClaudeVersion: m.info.ClaudeVersion, LastSeen: m.lastSeen, NodeVersion: m.info.Version, Engine: cmp.Or(m.engine, proto.EngineHerdr),
			Tailcat: m.cfg.TailcatAddr != ""}
		s.Engines = []proto.Engine{}
		if m.online || warm {
			s.Engines = append(s.Engines, m.info.Engines...)
		}
		s.NodeMismatch = m.online && nodeMismatch(m.hub.version, m.info.Version)
		can, why := m.canDeployLocked()
		s.CanDeploy, s.DeployReason = &can, why
	}
	s.Online, s.Error = m.online || warm, m.err
	if m.lastSeen.After(s.LastSeen) { // the session lists the hub got since the manager's last report
		s.LastSeen = m.lastSeen
	}
	if s.Engines == nil {
		s.Engines = []proto.Engine{}
	}
	s.Computer, s.Browser = computer, browser // the system is in the manager's report (its monitoring)
	return s
}

// canDeployLocked is whether the manager can update the machine's node from here, before it said (its own rules,
// internal/devices deploy_rpc.go): the hub machine's goes with the hub; one reached over its link or connector needs a
// node that updates itself; an offline one waits.
func (m *Machine) canDeployLocked() (bool, string) {
	if m.cfg.Kind == "local" {
		return false, i18n.T(m.hub.userLang(), "hub-machine.deploy_local")
	}
	if (m.cfg.Kind == kindLink || m.cfg.Kind == kindService || m.derived) && !m.info.NodeUpdate {
		if m.info.NodeUpdateReason != "" {
			return false, m.info.NodeUpdateReason
		}
		return false, i18n.T(m.hub.userLang(), "hub-machine.deploy_upgrade_first")
	}
	if !m.online {
		return false, i18n.T(m.hub.userLang(), "hub-machine.deploy_offline")
	}
	return true, ""
}

// reconnect has the manager connect the machine's node again.
func (m *Machine) reconnect() {
	if err := m.hub.callBus(context.Background(), m.ownerOrCarrier(), devicewire.MethodReconnect, map[string]any{"machine_id": m.cfg.ID}, nil); err != nil {
		m.hub.logf("[%s] reconnect: %v", m.cfg.ID, err)
	}
}

func (m *Machine) call(ctx context.Context, method string, params any, out any) error {
	m.mu.Lock()
	p := m.peer
	info := m.info
	m.mu.Unlock()
	if info.Version != "" && !info.ModelConfiguration && (method == "assistant.send" || method == "assistant.warm" || method == "session.start" || method == "session.send") {
		raw, _ := json.Marshal(params)
		var fields struct {
			Model       string `json:"model"`
			Fingerprint string `json:"model_fingerprint"`
		}
		_ = json.Unmarshal(raw, &fields)
		if fields.Model != "" || fields.Fingerprint != "" {
			return &rpc.Error{Code: proto.CodeNodeOutdated, Message: i18n.T(m.hub.contextLang(ctx), "hub-machine.node_no_model_config")}
		}
	}
	if p == nil {
		return fmt.Errorf("%s: %w", m.cfg.Name, errOffline)
	}
	select {
	case <-p.Done(): // closed, a new connection on its way: nothing was sent
		return fmt.Errorf("%s: %w", m.cfg.Name, errOffline)
	default:
	}
	return p.Call(ctx, method, params, out)
}

// reachable: a call would go out now (call answers errOffline otherwise).
func (m *Machine) reachable() bool {
	m.mu.Lock()
	p := m.peer
	managed, connected := m.managed, m.connected
	m.mu.Unlock()
	if managed {
		return connected && m.hub.devicesUp()
	}
	if p == nil {
		return false
	}
	select {
	case <-p.Done():
		return false
	default:
		return true
	}
}

// notify sends the node a one-way event; events arrive in the order sent.
func (m *Machine) notify(event string, params any) error {
	m.mu.Lock()
	p := m.peer
	m.mu.Unlock()
	if p == nil {
		return fmt.Errorf("%s: %w", m.cfg.Name, errOffline)
	}
	return p.Notify(event, params)
}

func (m *Machine) sessionList() []proto.Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]proto.Session, len(m.sessions))
	copy(out, m.sessions)
	return out
}

func (m *Machine) session(id string) (proto.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.SessionID == id {
			return s, true
		}
	}
	return proto.Session{}, false
}

func (m *Machine) addWatcher(ctx context.Context, id string) {
	m.mu.Lock()
	m.watchers[id]++
	m.mu.Unlock()
	m.syncWatch(ctx, id)
}

func (m *Machine) removeWatcher(id string) {
	m.mu.Lock()
	m.watchers[id]--
	if m.watchers[id] <= 0 {
		delete(m.watchers, id)
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), watchCallTimeout)
	defer cancel()
	m.syncWatch(ctx, id)
}

// watchCallTimeout bounds each session.watch / session.unwatch call, so a node that never answers holds a session's
// watchLock for at most this long. A variable for the tests.
var watchCallTimeout = 10 * time.Second

// nodeWatchState is what the node was told about a session's watch. The zero value is not watched.
type nodeWatchState int

const (
	nodeUnwatched nodeWatchState = iota
	nodeWatching
	nodeWatchUnknown // the last session.watch / session.unwatch failed: either may hold, the next sync sends again
)

// watchLock takes the lock that orders what the node is asked about session id's watch, giving up when ctx ends
// first (false). The returned func releases it.
func (m *Machine) watchLock(ctx context.Context, id string) (func(), bool) {
	m.mu.Lock()
	if m.watchMu == nil {
		m.watchMu = map[string]chan struct{}{}
	}
	sem := m.watchMu[id]
	if sem == nil {
		sem = make(chan struct{}, 1)
		m.watchMu[id] = sem
	}
	m.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	case <-ctx.Done():
		return nil, false
	}
}

// syncWatch makes the node watch session id when an app is subscribed to it and stop when none is. The calls for one
// session go one at a time, and each looks at the subscriber count when its turn comes: the watch of an app that
// subscribes while the last one's unwatch is on its way goes after it, not before (the node ignores a watch for a
// session it watches, so the unwatch arriving second would leave the subscriber with no live items).
//
// A caller whose ctx ends while another call holds the turn gives up waiting: the holder looks at the count again
// when its call is done and sends what changed meanwhile (syncWatchLocked), so no change is lost.
func (m *Machine) syncWatch(ctx context.Context, id string) {
	unlock, ok := m.watchLock(ctx, id)
	if !ok {
		return
	}
	defer unlock()
	m.syncWatchLocked(ctx, id)
}

// syncWatchLocked is syncWatch with session id's watchLock held. It sends calls until the node was told what the
// subscriber count wants or a call fails (the next sync sends again then).
func (m *Machine) syncWatchLocked(ctx context.Context, id string) {
	for first := true; ; first = false {
		if !first {
			// A later round is for subscribers that came or went meanwhile, not for this caller's connection.
			ctx = context.Background()
		}
		if !m.syncWatchOnce(ctx, id) {
			return
		}
	}
}

// syncWatchOnce sends the one call session id's watch needs now, if any, and records what it left. It reports whether
// a call went out and succeeded (another may be needed: the count may have changed while it was on its way).
func (m *Machine) syncWatchOnce(ctx context.Context, id string) bool {
	m.mu.Lock()
	want := m.watchers[id] > 0
	state := m.nodeWatch[id]
	since := m.lastSeq[id]
	m.mu.Unlock()
	var method string
	var params map[string]any
	switch {
	case want && state != nodeWatching:
		method, params = "session.watch", map[string]any{"session_id": id, "since_seq": since}
	case !want && state != nodeUnwatched:
		method, params = "session.unwatch", map[string]any{"session_id": id}
		ctx = context.Background() // not the leaving app's connection
	default:
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, watchCallTimeout)
	defer cancel()
	err := m.call(ctx, method, params, nil)
	next := nodeUnwatched
	switch {
	case err != nil:
		next = nodeWatchUnknown
		if !errors.Is(err, errOffline) {
			m.hub.logf("[%s] %s %s: %v", m.cfg.ID, method, id, err)
		}
	case want:
		next = nodeWatching
	}
	m.mu.Lock()
	if m.nodeWatch == nil {
		m.nodeWatch = map[string]nodeWatchState{}
	}
	if next == nodeUnwatched {
		delete(m.nodeWatch, id)
	} else {
		m.nodeWatch[id] = next
	}
	m.mu.Unlock()
	return err == nil
}

// watchAgainFresh syncs session id's watch with a manager that is new: it watches nothing yet, whatever the old one
// was told.
func (m *Machine) watchAgainFresh(ctx context.Context, id string) {
	unlock, ok := m.watchLock(ctx, id)
	if !ok {
		m.hub.logf("[%s] watch %s again: an earlier session.watch / session.unwatch is still on its way", m.cfg.ID, id)
		return
	}
	defer unlock()
	m.mu.Lock()
	delete(m.nodeWatch, id)
	m.mu.Unlock()
	m.syncWatchLocked(ctx, id)
}

func (m *Machine) onEvent(event string, params json.RawMessage) {
	switch event {
	case "fs.changed": // a folder or Git directory a web panel has open changed (fswatch.go)
		var p struct {
			Dirs []string `json:"dirs"`
			Git  []string `json:"git"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.hub.fsChanged(m.cfg.ID, p.Dirs, p.Git)
		}
	case "sessions.revs":
		var p struct {
			Revs map[string]int64 `json:"revs"`
		}
		if json.Unmarshal(params, &p) != nil || len(p.Revs) == 0 {
			return
		}
		m.mu.Lock()
		for i := range m.sessions {
			if r, ok := p.Revs[m.sessions[i].SessionID]; ok {
				m.sessions[i].Rev = r
			}
		}
		m.mu.Unlock()
		// Every app, not only those watching: this is what lets them pull a session before it is opened.
		m.hub.broadcast(map[string]any{"type": "revs", "machine_id": m.cfg.ID, "revs": p.Revs})
	case "session.items":
		var p struct {
			SessionID string       `json:"session_id"`
			Items     []proto.Item `json:"items"`
			itemSpan
		}
		// A batch without items still matters when it has a span: it moves the apps' "complete up to" forward.
		if json.Unmarshal(params, &p) != nil || (len(p.Items) == 0 && p.ToSeq == nil) {
			return
		}
		last := 0
		if len(p.Items) > 0 {
			last = p.Items[len(p.Items)-1].Seq
		}
		if p.ToSeq != nil {
			last = max(last, *p.ToSeq)
		}
		m.mu.Lock()
		if last > m.lastSeq[p.SessionID] {
			m.lastSeq[p.SessionID] = last
		}
		m.mu.Unlock()
		p.Items = m.hub.withInjected(p.Items)
		m.hub.itemsToSubscribers(m.cfg.ID, p.SessionID, p.Items, p.itemSpan)
	case "session.screen":
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		m.hub.toSubscribers(m.cfg.ID, p.SessionID, screenPush{Type: "screen", MachineID: m.cfg.ID, SessionID: p.SessionID,
			Text: p.Text, Status: p.Status, TS: time.Now().UTC()})
	case "session.live": // a working Claude's status line as it is now: not kept, not replayed (docs/protocol.md「实时状态」)
		var p struct {
			SessionID string             `json:"session_id"`
			Live      *proto.SessionLive `json:"live"`
		}
		if json.Unmarshal(params, &p) == nil && p.SessionID != "" {
			m.hub.toSubscribersWith(m.cfg.ID, p.SessionID, proto.FeatureSessionLive,
				livePush{Type: "live", MachineID: m.cfg.ID, SessionID: p.SessionID, Live: p.Live})
		}
	case "terminal.frame":
		m.hub.onTerminalFrame(m.cfg.ID, params)
	case "herdr.client.data":
		m.hub.onHerdrClientData(m.cfg.ID, params)
	case "herdr.client.end":
		m.hub.onHerdrClientEnd(m.cfg.ID, params)
	case "herdr.focus.changed":
		m.hub.onHerdrFocus(m.cfg.ID, params)
	case "terminal.end":
		var p struct {
			SessionID string `json:"session_id"`
			Error     string `json:"error"`
			Control   bool   `json:"control"`
			Taken     bool   `json:"taken"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		if p.Control {
			m.hub.terminalControlEnded(m.cfg.ID, p.SessionID, p.Error, p.Taken)
		} else {
			m.hub.terminalEnded(m.cfg.ID, p.SessionID, p.Error, nil)
		}
	case "session.delta":
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.hub.rememberLiveText(m.cfg.ID, p.SessionID, p.Text)
			m.hub.toSubscribers(m.cfg.ID, p.SessionID, deltaPush{Type: "delta", MachineID: m.cfg.ID, SessionID: p.SessionID, Text: p.Text})
		}
	case "codex.inventory":
		m.rememberCodexInventory(params)
	case "session.echo":
		m.recordEcho(params)
	case "hook.event": // what it says of the session's turn is the manager's (assistant/fleet.go); the apps see it
		var p map[string]any
		if json.Unmarshal(params, &p) == nil {
			p["type"] = "hook_event"
			p["machine_id"] = m.cfg.ID
			m.hub.broadcast(p)
		}
	}
}

// onRequest answers what is the hub's of a node's requests (devices.request): personad notify on the machine. The
// interactions, the MCP tools and assistant.lark are the manager's (assistant/fleet.go).
func (m *Machine) onRequest(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "notify": // personad notify on this machine (node/scriptcli.go)
		return m.scriptNotify(params)
	case assistantwire.MethodViewCheck:
		return m.checkAssistantView(params)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// nodeMismatch: the node's jarvisd is another build than the hub's. Builds are named <commit>[-dirty]-<time>
// (deploy.sh); dev builds ("dev", empty) are never flagged. The local node is the hub binary itself.
func nodeMismatch(hub, node string) bool {
	if hub == "" || node == "" || hub == "dev" || node == "dev" {
		return false
	}
	return hub != node
}

func (m *Machine) isOnline() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online
}
