package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/streams"
)

// A machine's connection to its node, over one of its paths at a time (mesh_paths.go): the run loop picks the best
// path that can be tried, connects (establish: the node answers node.info), serves the connection until it ends
// (serve), and starts over. While a connection is up, a path that stays clearly better for mesh.path_switch_seconds is
// connected next to it; once its node answers, it takes over and the old one is let go — the apps see no offline in
// between.

var errSwitched = errors.New("moved to a better path")

// nodeConn is one connection to the node, its node.info answered.
type nodeConn struct {
	path   *nodePath
	via    string // the transport it took: local | ssh | tailcat | link | service | hub
	pipe   *nodePipe
	peer   *rpc.Peer
	mux    *streams.Mux
	info   proto.NodeInfo
	served chan error
	cancel context.CancelFunc
}

func (c *nodeConn) close() {
	c.peer.Close(errors.New("closed"))
	c.cancel()
	c.mux.CloseAll()
	c.pipe.close()
}

// run keeps the machine connected through its best path until ctx ends.
func (m *Machine) run(ctx context.Context) {
	var conn *nodeConn
	for ctx.Err() == nil {
		if conn == nil {
			if why := m.hub.parkReason(m); why != "" {
				if why == "removed" {
					return
				}
				m.park(why)
				m.waitChange(ctx, 0)
				continue
			}
			if d := m.hub.meshSettling(); d > 0 {
				// Just started with peers: the group may have another center by now (a larger term); a member must not
				// grab its computers before it hears so.
				m.park("等组里的其他 hub 连上")
				m.waitChange(ctx, d)
				continue
			}
			m.unpark()
			p, wait := m.hub.choosePath(m)
			if p == nil {
				m.noPath()
				m.waitChange(ctx, wait)
				continue
			}
			c, err := m.establish(ctx, p)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				m.hub.pathEnded(p, false, err, 0, false)
				m.markOffline(err)
				continue
			}
			conn = c
		}
		start := time.Now()
		err := m.serve(ctx, conn)
		next := m.takeNext()
		planned := next != nil
		m.hub.pathEnded(conn.path, true, err, time.Since(start), planned)
		conn.close()
		m.detach(err, planned)
		conn = next
	}
	if conn != nil {
		conn.close()
	}
	m.mu.Lock()
	if m.next != nil {
		m.next.close()
		m.next = nil
	}
	m.mu.Unlock()
}

// wake makes the run loop look again (its paths changed, a link came, the hub's role changed).
func (m *Machine) wake() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// waitChange waits for a reason to look again: a kick, a change of the machines or their paths, or (wait > 0) that
// long; at most a minute.
func (m *Machine) waitChange(ctx context.Context, wait time.Duration) {
	if wait <= 0 || wait > time.Minute {
		wait = time.Minute
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-m.kick:
	case <-m.hub.fleetChanged():
	case <-m.hub.meshWait():
	case <-t.C:
	}
}

// park: this machine is not connected from here now (why), and says so.
func (m *Machine) park(why string) {
	m.mu.Lock()
	changed := m.parked != why
	m.parked = why
	if changed {
		m.err = why
	}
	m.mu.Unlock()
	if changed {
		m.hub.broadcastMachines()
	}
}

func (m *Machine) unpark() {
	m.mu.Lock()
	was := m.parked != ""
	m.parked = ""
	if was {
		m.err = ""
	}
	m.mu.Unlock()
}

// noPath: no path can be tried now; the machine shows why the last one failed (or what it waits for).
func (m *Machine) noPath() {
	why := ""
	for _, p := range m.hub.pathsOf(m) {
		if st := m.hub.pathState(p); st.err != "" {
			why = st.err
			break
		}
	}
	if why == "" {
		return
	}
	m.mu.Lock()
	changed := !m.online && m.err != why
	if changed {
		m.err = why
	}
	m.mu.Unlock()
	if changed {
		m.hub.broadcastMachines()
	}
}

// establish connects through p: each of its transports in turn (ssh, then tailcat), until the node answers node.info.
func (m *Machine) establish(ctx context.Context, p *nodePath) (*nodeConn, error) {
	vias := []string{viaHub}
	if p.src != nil {
		vias = p.src.transports()
	}
	var failed []string
	for _, via := range vias {
		c, err := m.establishVia(ctx, p, via)
		if err == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		failed = append(failed, via+"："+err.Error())
		if via == "ssh" && p.src.cfg.TailcatAddr != "" {
			p.src.noteDirectFailed()
		}
	}
	if len(failed) == 1 {
		_, msg, _ := strings.Cut(failed[0], "：")
		return nil, errors.New(msg)
	}
	return nil, errors.New(strings.Join(failed, "；"))
}

func (m *Machine) establishVia(ctx context.Context, p *nodePath, via string) (*nodeConn, error) {
	cctx, cancel := context.WithCancel(ctx)
	var l *nodePipe
	var err error
	if via == viaHub {
		l, err = m.hub.openThroughHub(cctx, p.hub, p.mid)
	} else {
		l, err = p.src.open(cctx, via)
	}
	if err != nil {
		cancel()
		if via == "tailcat" {
			p.src.tailcatReset()
		}
		return nil, err
	}
	peer := rpc.New("h", l.w)
	mux := streams.New(peer.Notify) // forwarded ports (ports.go)
	peer.OnEvent = func(event string, params json.RawMessage) {
		if !mux.Event(event, params) {
			m.onEvent(event, params)
		}
	}
	peer.OnRequest = m.onRequest
	c := &nodeConn{path: p, via: via, pipe: l, peer: peer, mux: mux, served: make(chan error, 1), cancel: cancel}
	go func() { c.served <- peer.Serve(cctx, l.r) }()

	ictx, icancel := context.WithTimeout(cctx, 40*time.Second)
	// The engine first, before anything else the node is asked: it then never lists or watches the wrong one.
	m.useEngine(ictx, peer)
	err = peer.Call(ictx, "node.info", map[string]any{}, &c.info)
	icancel()
	if err != nil {
		c.close()
		if via == "tailcat" {
			p.src.tailcatReset()
		}
		if msg := l.lastErr(); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, err
	}
	return c, nil
}

// serve makes c the machine's connection and serves it until it ends (or a better path took over: m.next).
func (m *Machine) serve(ctx context.Context, c *nodeConn) error {
	p := c.path
	m.hub.pathConnected(p)
	m.mu.Lock()
	m.peer, m.info, m.online, m.err, m.lastSeen = c.peer, c.info, true, "", time.Now().UTC()
	m.endWarmLocked()
	m.streams = c.mux
	m.via, m.path = c.via, ""
	m.active, m.transport, m.uid = p, c.via, c.info.MachineUID
	if c.via == "ssh" && p.src == m {
		m.directFailed = time.Time{}
	}
	onlineAt := time.Now()
	rewatch := map[string]int{}
	for id, n := range m.watchers {
		if n > 0 {
			rewatch[id] = m.lastSeq[id]
		}
	}
	m.mu.Unlock()
	if c.via == "ssh" && p.src != nil && p.src != m {
		p.src.mu.Lock()
		p.src.directFailed = time.Time{}
		p.src.mu.Unlock()
	}
	info := c.info
	where := c.via
	if c.via == viaHub {
		where = "hub " + m.hub.hubNameOf(p.hub)
	}
	m.hub.logf("[%s] connected over %s: %s %s/%s herdr %s claude %s", m.cfg.ID, where, info.Hostname, info.OS, info.Arch, info.HerdrVersion, info.ClaudeVersion)
	m.tellLinks(true, "")
	m.hub.onSystem(m.cfg.ID, info.System)
	if nodeMismatch(m.hub.version, info.Version) {
		m.hub.logf("[%s] node %s is not the hub's build %s: update it", m.cfg.ID, info.Version, m.hub.version)
	}
	m.hub.broadcastMachines()
	m.hub.connectors.MachineOnline(m.cfg.ID)
	go m.hub.refreshComputer(ctx, m)   // only when the plugin cua is on for it (computer.go)
	go m.hub.tellNoLock(ctx, m)        // 「插件开着时不自动锁屏」 (computer_nolock.go)
	go m.hub.refreshBrowser(ctx, m)    // and ego (browser.go)
	m.hub.learnUID(p, info.MachineUID) // the paths reaching the same computer become one machine (mesh_paths.go)
	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	if c.via == "tailcat" && p.src == m {
		go m.watchTailcat(sctx, c.peer, c.pipe.activity)
	}

	m.configureNode(ctx, c.peer)
	var list struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if err := c.peer.Call(ctx, "sessions.list", map[string]any{}, &list); err == nil {
		m.setSessions(list.Sessions)
		m.nodeUpdateConnected(ctx, c.peer, info)
	}
	for id, since := range rewatch {
		_ = c.peer.Call(ctx, "session.watch", map[string]any{"session_id": id, "since_seq": since}, nil)
	}
	go m.watchPaths(sctx, c)
	if info.ClaudeVersion == "" {
		go m.retryClaudeVersion(sctx, c.peer)
	}

	// The connection ends when the node's output does, or when the peer is closed (reconnect, a better path): then
	// an ssh process is still there and its output never ends — waiting only for it left the machine "online" with
	// every call failing until the hub restarted. Returning cancels the connection's context, which kills the process.
	var err error
	select {
	case err = <-c.served:
	case <-c.peer.Done():
		if err = c.peer.Err(); err == nil {
			err = errors.New("reconnect requested")
		}
	case <-ctx.Done():
		err = ctx.Err()
	}
	if c.via == "ssh" && p.src != nil && time.Since(onlineAt) > 10*time.Minute { // ssh held: going back to it worked
		p.src.mu.Lock()
		p.src.flaps, p.src.switchedBack = 0, time.Time{}
		p.src.mu.Unlock()
	}
	if errors.Is(err, io.EOF) {
		if msg := c.pipe.lastErr(); msg != "" {
			err = fmt.Errorf("node exited: %s", msg)
		} else {
			err = errors.New("node exited")
		}
	}
	return err
}

// watchPaths, while c is the connection, leaves it for a clearly better path once that has stayed better long
// enough (connecting the new one first), and lets it go when its path now reaches another computer or this machine
// is to be parked.
func (m *Machine) watchPaths(ctx context.Context, c *nodeConn) {
	since := map[string]time.Time{}
	t := time.NewTicker(pathCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.hub.fleetChanged():
		}
		if ctx.Err() != nil {
			return
		}
		if why := m.hub.parkReason(m); why != "" || !m.hasPath(c.path) {
			if why == "" {
				why = "这条路通向的是另一台电脑"
			}
			c.peer.Close(errors.New(why))
			return
		}
		q := m.hub.betterPath(m, c.path, since)
		if q == nil {
			continue
		}
		m.hub.logf("[%s] trying a better path: %s instead of %s", m.cfg.ID, pathName(m.hub, q), pathName(m.hub, c.path))
		next, err := m.establish(ctx, q)
		if err != nil {
			m.hub.pathEnded(q, false, err, 0, false)
			m.hub.logf("[%s] the better path did not work: %v", m.cfg.ID, err)
			delete(since, q.key)
			continue
		}
		m.mu.Lock()
		m.next = next
		m.mu.Unlock()
		c.peer.Close(errSwitched)
		return
	}
}

func pathName(h *Hub, p *nodePath) string {
	if p.via == viaHub {
		return "hub " + h.hubNameOf(p.hub)
	}
	return p.via + " (" + p.src.cfg.ID + ")"
}

func (m *Machine) hasPath(p *nodePath) bool {
	for _, q := range m.hub.pathsOf(m) {
		if q.key == p.key {
			return true
		}
	}
	return false
}

func (m *Machine) takeNext() *nodeConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.next
	m.next = nil
	return n
}

// detach lets the connection that ended go: its terminals and the questions waiting on it end (hooks with resumable
// ones come back through the next connection). Unless another connection takes over at once (planned), the machine is
// offline.
func (m *Machine) detach(err error, planned bool) {
	if m.hub.stopping.Load() {
		// The hub is stopping for a restart: the next one shows the machine as it was while its node reconnects
		// (warm.go). Telling the apps it went offline, or ending its questions, would only be noise.
		m.mu.Lock()
		m.peer, m.streams, m.active, m.transport = nil, nil, nil, ""
		m.mu.Unlock()
		return
	}
	m.hub.forgetLiveText(m.cfg.ID) // the node that was generating it is gone: the transcript has what was finished
	m.hub.terminalsOffline(m.cfg.ID)
	m.mu.Lock()
	m.peer, m.streams = nil, nil
	m.active, m.transport = nil, ""
	m.todoTokensMu.Lock()
	m.lastTodoTokens = nil
	m.todoTokensMu.Unlock()
	if !planned && m.online && reconnectGrace > 0 && m.cfg.Kind != kindLink {
		// A node the hub reaches itself that comes straight back (a resident node exec'ing a new binary, a relay
		// replaced) is not news: the machine shows as it was for reconnectGrace, then offline as below if it is still
		// gone (warm.go). A link going away is the computer leaving: offline at once (TestLinkMachineComesOnline…).
		m.online = false
		if err != nil {
			m.err = err.Error()
		}
		ch := make(chan struct{})
		m.warmUntil, m.warmed = time.Now().Add(reconnectGrace), ch
		m.mu.Unlock()
		m.hub.logf("[%s] disconnected: %v (shown as online for %s while it reconnects)", m.cfg.ID, err, reconnectGrace)
		time.AfterFunc(reconnectGrace, func() { m.warmExpired(ch) })
		m.hub.cancelMachineInteractions(m.cfg.ID)
		return
	}
	if !planned {
		m.online = false
		m.via, m.path = "", ""
		if err != nil {
			m.err = err.Error()
		}
		for i := range m.sessions {
			if m.sessions[i].Status != proto.StatusOffline {
				m.sessions[i].Status = proto.StatusUnknown
			}
		}
	}
	m.mu.Unlock()
	if planned {
		m.hub.logf("[%s] %v", m.cfg.ID, errSwitched)
	} else {
		m.hub.logf("[%s] offline: %v", m.cfg.ID, err)
		if err != nil {
			m.tellLinks(false, err.Error())
		}
		m.hub.broadcastMachines()
	}
	m.hub.cancelMachineInteractions(m.cfg.ID)
}

// markOffline: a connection could not be made; the machine shows why.
func (m *Machine) markOffline(err error) {
	if m.hub.stopping.Load() {
		return // a connection that failed because the hub is stopping (detach)
	}
	m.mu.Lock()
	was := m.online
	m.online = false
	m.todoTokensMu.Lock()
	m.lastTodoTokens = nil
	m.todoTokensMu.Unlock()
	if err != nil {
		m.err = err.Error()
	}
	m.mu.Unlock()
	if was {
		m.hub.cancelMachineInteractions(m.cfg.ID)
	}
	m.hub.logf("[%s] offline: %v", m.cfg.ID, err)
	if err != nil {
		m.tellLinks(false, err.Error())
	}
	m.hub.broadcastMachines()
}

// setOffline is detach for callers outside the run loop (tests that end a connection by hand).
func (m *Machine) setOffline(err error) { m.detach(err, false) }

func (m *Machine) activePath() *nodePath {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *Machine) activePathKey() string {
	if p := m.activePath(); p != nil {
		return p.key
	}
	return ""
}

func (m *Machine) transportNow() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.transport
}

func (m *Machine) nodeUID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uid
}

// tellLinks says on the links of this computer's jarvis-links (its own entry's and those of entries with the same
// computer) whether the hub got through to the resident node.
func (m *Machine) tellLinks(online bool, err string) {
	for _, p := range m.hub.pathsOf(m) {
		if p.src != nil && p.src.cfg.Kind == kindLink {
			p.src.tellLink(online, err)
		}
	}
}

// reachableHere: this hub can try its hub.json entry's way to the node now as far as it knows, and its last round
// trip there (ms, 0 unknown). A member says so in its record; the center uses it to choose.
func (m *Machine) reachableHere() (bool, int) {
	if m.dialNode != nil || m.nodeCmd != nil {
		return true, 0
	}
	switch m.cfg.Kind {
	case "local":
		return true, 0
	case kindLink:
		l := m.currentLink()
		if l == nil {
			return false, 0
		}
		return true, l.rttMS()
	case kindService:
		return m.serviceReachable()
	}
	return true, 0 // ssh, tailcat: known only by trying
}

// unavailableWhy is what keeps the entry's way from being tried.
func (m *Machine) unavailableWhy() string {
	switch m.cfg.Kind {
	case kindLink:
		m.mu.Lock()
		l, down := m.link, m.linkDown
		m.mu.Unlock()
		if l != nil && l.ctx.Err() != nil { // going away: why
			if c := context.Cause(l.ctx); c != nil {
				return c.Error()
			}
		}
		if down != nil {
			return down.Error()
		}
		return "等这台电脑的 Persona Link 连上来"
	case kindService:
		return m.serviceWhy()
	}
	return ""
}

// forMesh is a hub.json entry as a member tells the center about it: no ssh host, tailcat address or binary path.
func (c MachineConfig) forMesh() MachineConfig {
	c.SSHHost, c.TailcatAddr, c.RemoteBin, c.Address, c.Proxy = "", "", "", "", ""
	return c
}

// connect makes one connection through the machine's own hub.json entry and serves it until it ends (tests drive a
// machine this way, without the run loop).
func (m *Machine) connect(ctx context.Context) error {
	c, err := m.establish(ctx, cfgPath(m))
	if err != nil {
		return err
	}
	err = m.serve(ctx, c)
	c.close()
	m.detach(err, false)
	return err
}

// claudeVersionRetry is how long the hub waits before asking a node again for its Claude version while node.info came
// back without one; the last wait repeats until the connection ends.
var claudeVersionRetry = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour}

// retryClaudeVersion asks the node for node.info again until it reports a Claude version, then shows it (#542). The
// node finds the version by running `claude --version` in a login shell, which fails now and then on connecting (the
// machine just woke, a slow ~/.zshrc, Claude updating itself); asked only then, the machine showed no version until it
// reconnected. A machine without Claude is asked again every hour while it stays connected.
func (m *Machine) retryClaudeVersion(ctx context.Context, peer *rpc.Peer) {
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(claudeVersionRetry[min(i, len(claudeVersionRetry)-1)]):
		}
		var info proto.NodeInfo
		cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		err := peer.Call(cctx, "node.info", map[string]any{}, &info)
		cancel()
		if err != nil || info.ClaudeVersion == "" {
			continue
		}
		m.mu.Lock()
		current := m.peer == peer
		if current {
			m.info.ClaudeVersion = info.ClaudeVersion
		}
		m.mu.Unlock()
		if current {
			m.hub.logf("[%s] claude %s", m.cfg.ID, info.ClaudeVersion)
			m.hub.broadcastMachines()
		}
		return
	}
}
