package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/streams"
)

var errOffline = apierr.ErrOffline

// Machine is the hub's connection to one node: a local subprocess, `ssh host jarvisd node --stdio`, a tailcat
// connection to its resident node, or a stream on the link the computer's jarvis-link keeps to the hub (kind link,
// link.go).
type Machine struct {
	cfg MachineConfig
	hub *Hub

	mu      sync.Mutex
	peer    *rpc.Peer
	streams *streams.Mux // the forwarded ports' connections on peer (ports.go)
	info    proto.NodeInfo
	// computer: what the node last said of Cua Driver (computer.status), while the plugin cua is on for it (computer.go);
	// computerAt: when the node answered it (or, from an older node, when it was asked), so an older answer never
	// replaces it (keepComputer).
	computer   *proto.Computer
	computerAt time.Time
	// browser, browserAt: the same for ego lite (browser.status, the plugin ego; browser.go).
	browser   *proto.Browser
	browserAt time.Time
	online    bool
	// warmUntil, warmed: shown as before a hub restart until then, while its node reconnects (warm.go); warmed is
	// closed when that ends.
	warmUntil time.Time
	warmed    chan struct{}
	err       string
	lastSeen  time.Time
	sessions  []proto.Session
	watchers  map[string]int // session id -> subscribed clients
	lastSeq   map[string]int // session id -> highest item seq seen
	hooked    map[string]bool
	kick      chan struct{}
	// deploying: the hub is sending this machine its node build (deploy.go); deployErr: why the last try failed.
	deploying   bool
	deployErr   string
	deployPhase string
	// via is how the connection runs (local | ssh | tailcat), path how tailcat's packets go (direct | relay).
	via, path string
	// directFailed: when ssh last failed to connect and tailcat was tried instead; switchedBack: when the hub last
	// went back to ssh from tailcat; flaps: how many times in a row that failed right away (tailcat.go).
	directFailed, switchedBack time.Time
	flaps                      int
	timings                    tailcatTimings
	// engineMu: one engine.use to the node at a time (engine.go).
	engineMu sync.Mutex
	link     *hubLink      // kind link: the link jarvis-link keeps open, nil while there is none
	linkDown error         // why the last link went away
	linkUp   chan struct{} // a link arrived
	// nodeCmd replaces the node command (tests).
	nodeCmd func(context.Context) *exec.Cmd
	// dialTailcat replaces dialing the node over tailcat, probeDirect checking that ssh works (tests).
	dialTailcat func(context.Context) (net.Conn, error)
	probeDirect func(context.Context) error
	// dialNode replaces every way to the node with a connection to an in-process one (tests).
	dialNode func(context.Context) (net.Conn, error)

	// derived: a computer only a member hub has in hub.json; the center reaches it through that hub (mesh_paths.go).
	derived bool
	// active: the path the connection runs over (nil while offline); transport: the way it took (ssh may fall back to
	// tailcat); uid: the machine_uid the node said; next: a connection made through a better path, to take over from
	// the one in use (machine_conn.go).
	active    *nodePath
	transport string
	uid       string
	next      *nodeConn
	parked    string // why the run loop does not connect now (mesh_paths.go parkReason)
	// svc: the connector of a computer of kind service (service.go).
	svc serviceState

	todoTokensMu      sync.Mutex
	todoTokensDirty   bool
	todoTokensSending bool
	lastTodoTokens    map[string]map[string]string
}

func newMachine(h *Hub, cfg MachineConfig) *Machine {
	m := &Machine{cfg: cfg, hub: h, watchers: map[string]int{}, lastSeq: map[string]int{}, kick: make(chan struct{}, 1),
		timings: defaultTailcatTimings, linkUp: make(chan struct{}, 1)}
	switch cfg.Kind {
	case kindLink:
		m.err = "waiting for Persona Link"
	case kindService:
		m.err = "waiting for the connector"
	}
	return m
}

func (m *Machine) snapshot() proto.Machine {
	engine := m.hub.engineOf(m.cfg.ID) // before m.mu: it may read settings
	computer := m.hub.computerOf(m)    // before m.mu: it takes it
	browser := m.hub.browserOf(m)
	m.mu.Lock()
	defer m.mu.Unlock()
	engines := []proto.Engine{} // what the node found: none while offline
	warm := m.warmLocked(time.Now())
	if m.online || warm {
		engines = append(engines, m.info.Engines...)
	}
	icon := m.cfg.Icon
	if icon == "" {
		icon = "laptop"
	}
	canDeploy, deployReason := m.canDeployLocked()
	return proto.Machine{
		ID: m.cfg.ID, Name: m.cfg.Name, Kind: m.cfg.Kind, Icon: icon, SSHHost: m.cfg.SSHHost, Online: m.online || warm, Error: m.err,
		Hostname: m.info.Hostname, OS: m.info.OS, Arch: m.info.Arch, Home: m.info.Home,
		HerdrVersion: m.info.HerdrVersion, ClaudeVersion: m.info.ClaudeVersion, LastSeen: m.lastSeen,
		NodeVersion: m.info.Version, NodeMismatch: m.online && nodeMismatch(m.hub.version, m.info.Version),
		Deploying: m.deploying, DeployError: m.deployErr,
		CanDeploy: &canDeploy, DeployReason: deployReason, DeployPhase: m.deployPhase,
		System:    m.hub.metrics.system(m.cfg.ID),
		Transport: m.via, TailcatPath: m.path, Tailcat: m.cfg.TailcatAddr != "",
		Engine: engine, Engines: engines, Computer: computer, Browser: browser, Link: linkInfo(m.link),
	}
}

func linkInfo(l *hubLink) *proto.MachineLink {
	if l == nil {
		return nil
	}
	return l.info()
}

func (m *Machine) reconnect() {
	m.mu.Lock()
	p := m.peer
	m.mu.Unlock()
	if p != nil {
		p.Close(errors.New("reconnect requested"))
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

func (m *Machine) command(ctx context.Context) *exec.Cmd {
	if m.nodeCmd != nil {
		return m.nodeCmd(ctx)
	}
	if m.cfg.Kind == "local" {
		self, _ := os.Executable()
		return exec.CommandContext(ctx, self, "node", "--stdio")
	}
	bin := m.cfg.RemoteBin
	if bin == "" {
		bin = "~/.jarvis/bin/jarvisd"
	}
	host := m.cfg.SSHHost
	if host == "" {
		host = m.cfg.ID
	}
	// Compression: the RPC is JSON (transcript pages, screens) and shrinks about 4x. Over a slow link (a machine
	// abroad at ~15 KB/s, 2026-10-31) an 87 KB page took 5.6 s and a 500-item page passed the hub's 30 s deadline;
	// on a LAN it costs next to nothing.
	return exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "Compression=yes",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", host, bin+" node --stdio")
}

// nodePipe is one way to a node: what it says, where to write, and why it failed (ssh's last stderr line). It is an
// ssh (or local) process's stdout and stdin, a tailcat connection, or a stream on a jarvis-link (dialLink); connectVia
// speaks the node protocol over each the same way.
type nodePipe struct {
	r       io.Reader
	w       io.Writer
	lastErr func() string
	close   func() // ends it and waits until it is gone
	// activity is when bytes last moved either way (tailcat only: watchTailcat pings only an idle connection).
	activity *activityConn
}

func (m *Machine) open(ctx context.Context, via string) (*nodePipe, error) {
	if m.dialNode != nil {
		c, err := m.dialNode(ctx)
		if err != nil {
			return nil, err
		}
		return &nodePipe{r: c, w: c, lastErr: func() string { return "" }, close: func() { _ = c.Close() }}, nil
	}
	if via == "link" {
		return m.dialLink(ctx)
	}
	if via == kindService {
		return m.dialService(ctx)
	}
	if via == "tailcat" {
		dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		c, err := m.tailcatDial(dctx)
		if err != nil {
			return nil, err
		}
		ac := &activityConn{Conn: c}
		ac.touch()
		return &nodePipe{r: ac, w: ac, lastErr: func() string { return "" }, close: func() { _ = c.Close() }, activity: ac}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := m.command(ctx)
	// Bound pipe draining if a node descendant inherits stdout or stderr.
	cmd.WaitDelay = shutdownStepTimeout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	var lastErr strings.Builder
	var errMu sync.Mutex
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			m.hub.logf("[%s] %s", m.cfg.ID, line)
			errMu.Lock()
			lastErr.Reset()
			lastErr.WriteString(line)
			errMu.Unlock()
		}
	}()
	return &nodePipe{r: stdout, w: stdin,
		// Asked once the link has failed: stdout can close before ssh's last stderr line is read, so give the reader a
		// moment to finish (TestBothWaysFailing lost "ssh: connect to host …" that way on a loaded machine).
		lastErr: func() string {
			select {
			case <-stderrDone:
			case <-time.After(2 * time.Second):
			}
			errMu.Lock()
			defer errMu.Unlock()
			return lastErr.String()
		},
		close: func() { cancel(); _ = cmd.Wait() },
	}, nil
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
			return &rpc.Error{Code: proto.CodeNodeOutdated, Message: "这台电脑的 node 还不支持统一模型配置，请更新 node"}
		}
	}
	if p == nil && m.waitWarm(ctx) { // the hub just restarted: its node is on its way back (warm.go)
		m.mu.Lock()
		p = m.peer
		m.mu.Unlock()
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
	m.mu.Unlock()
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

func (m *Machine) setSessions(list []proto.Session) {
	for i := range list {
		list[i].MachineID = m.cfg.ID
	}
	m.mu.Lock()
	before := make(map[string]string, len(m.sessions))
	for _, s := range m.sessions {
		before[s.SessionID] = s.Status
	}
	now := time.Now()
	for _, s := range list {
		// Any agent (herdr or headless, started by Persona or not) finishing a turn may notify the phone (notify.go).
		prev, key := before[s.SessionID], m.cfg.ID+"/"+s.SessionID
		switch {
		case s.Status == proto.StatusWorking && prev != proto.StatusWorking:
			m.hub.turns.working(key, now)
			go m.hub.workStarted(m.cfg.ID, s.SessionID, now)
			go m.hub.asst().TurnStarted(context.Background(), assistantwire.SessionRef{MachineID: m.cfg.ID, SessionID: s.SessionID})
		case prev == proto.StatusWorking && (s.Status == proto.StatusDone || s.Status == proto.StatusIdle):
			go m.notifyFinished(s.SessionID, m.hub.turns.ended(key, now))
			// A new assistant conversation is named after its first turn; a task from one may be done.
			go m.hub.asst().TurnEnded(context.Background(), assistantwire.SessionRef{MachineID: m.cfg.ID, SessionID: s.SessionID})
			go m.hub.channelTurnEnded(m.cfg.ID, s.SessionID)
			go m.hub.workTurnEnded(m, s.SessionID) // triage the turn: done, or blocked on what (work.go)
		}
	}
	m.sessions = list
	m.lastSeen = time.Now().UTC()
	m.mu.Unlock()
	m.hub.sessionsChanged(m)
	// Messages from other conversations, for those no longer running.
	m.hub.asst().SessionsChanged(context.Background(), assistantwire.DeviceSessions{MachineID: m.cfg.ID, Sessions: list})
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
	first := m.watchers[id] == 1
	since := m.lastSeq[id]
	m.mu.Unlock()
	if first {
		if err := m.call(ctx, "session.watch", map[string]any{"session_id": id, "since_seq": since}, nil); err != nil {
			m.hub.logf("[%s] watch %s: %v", m.cfg.ID, id, err)
		}
	}
}

func (m *Machine) removeWatcher(id string) {
	m.mu.Lock()
	m.watchers[id]--
	last := m.watchers[id] <= 0
	if last {
		delete(m.watchers, id)
	}
	m.mu.Unlock()
	if last {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.call(ctx, "session.unwatch", map[string]any{"session_id": id}, nil)
	}
}

func (m *Machine) onEvent(event string, params json.RawMessage) {
	switch event {
	case "sessions.changed":
		var p struct {
			Sessions []proto.Session `json:"sessions"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.setSessions(p.Sessions)
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
		m.hub.syncPlanInteractions(m)
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
		m.hub.itemsToSubscribers(m.cfg.ID, p.SessionID, p.Items, p.itemSpan)
		m.hub.channelItems(m.cfg.ID, p.SessionID, p.Items)
	case "session.screen":
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		m.hub.toSubscribers(m.cfg.ID, p.SessionID, map[string]any{"type": "screen", "machine_id": m.cfg.ID, "session_id": p.SessionID,
			"text": p.Text, "status": p.Status, "ts": time.Now().UTC()})
	case "terminal.frame":
		m.hub.onTerminalFrame(m.cfg.ID, params)
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
			m.hub.toSubscribers(m.cfg.ID, p.SessionID, map[string]any{"type": "delta", "machine_id": m.cfg.ID, "session_id": p.SessionID, "text": p.Text})
			m.hub.channelDelta(m.cfg.ID, p.SessionID, p.Text)
		}
	case "state.put":
		var p struct {
			SessionID string         `json:"session_id"`
			State     db.NodeSession `json:"state"`
		}
		if json.Unmarshal(params, &p) == nil && p.SessionID != "" && m.hub.store != nil {
			if err := m.hub.store.PutNodeSession(context.Background(), m.cfg.ID, p.SessionID, p.State); err != nil {
				m.hub.logf("[%s] save session state: %v", m.cfg.ID, err)
			}
		}
	case "codex.inventory":
		m.rememberCodexInventory(params)
	case "interaction.dismiss":
		var p struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.hub.recordEnd(p.ID, "dismissed", nil)
			m.hub.finishInteraction(p.ID)
		}
	case "claude.dirs":
		m.rememberClaudeDirs(params)
	case "session.echo":
		m.recordEcho(params)
	case "usage":
		m.hub.usage.apply(m.cfg.ID, params, time.Now())
	case "metrics":
		m.hub.onMetrics(m.cfg.ID, params)
	case "codex.interactions": // the Codex questions and approvals open on this machine (codexcards.go)
		m.hub.syncCodexInteractions(m, params)
	case "interaction.cancel":
		var p struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.hub.recordEnd(p.ID, "cancelled", nil) // or the next hub would take it for one a hook may still claim
			m.hub.finishInteraction(p.ID)
		}
	case "hook.event":
		var p map[string]any
		if json.Unmarshal(params, &p) == nil {
			if sid, _ := p["session_id"].(string); sid != "" {
				m.mu.Lock()
				if m.hooked == nil {
					m.hooked = map[string]bool{}
				}
				m.hooked[sid] = true
				m.mu.Unlock()
			}
			p["type"] = "hook_event"
			p["machine_id"] = m.cfg.ID
			m.hub.broadcast(p)
		}
	}
}

func (m *Machine) onRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "interaction.create":
		var p struct {
			Interaction proto.Interaction `json:"interaction"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		p.Interaction.MachineID = m.cfg.ID
		// A write through a skill's gated command (node/larkgate.go): its plugin names what the ids are.
		if s, ok := m.hub.plugins.assistantSummary(ctx, p.Interaction.ToolName, p.Interaction.Summary); ok {
			p.Interaction.Summary = s
		}
		return m.hub.waitInteraction(ctx, p.Interaction)
	case "assistant.lark":
		// The skills' gated commands' credentials go to the assistant's machine only (its process runs them); so does
		// how to run lark-cli (its login stays on that machine, in lark-cli's own store).
		if m.cfg.ID != m.hub.assistantConfig(ctx).MachineID {
			return proto.AssistantLark{}, nil
		}
		return m.hub.assistantLark(ctx)
	case "state.load":
		out := map[string]db.NodeSession{}
		if m.hub.store != nil {
			var err error
			if out, err = m.hub.store.NodeSessions(ctx, m.cfg.ID); err != nil {
				return nil, err
			}
		}
		return map[string]any{"sessions": out}, nil
	case "mcp.tools":
		var p struct {
			Assistant bool `json:"assistant"`
		}
		_ = json.Unmarshal(params, &p)
		return map[string]any{"tools": m.hub.mcpTools(p.Assistant)}, nil
	case "mcp.call":
		var p struct {
			Name      string          `json:"name"`
			Args      json.RawMessage `json:"args"`
			SessionID string          `json:"session_id"`
			Assistant bool            `json:"assistant"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if m.hub.connectors.Offers(p.Name) && (p.Assistant || m.hub.connectors.ForAgents(p.Name)) {
			return m.hub.callConnector(ctx, m.cfg.ID, p.Name, p.Args), nil
		}
		if p.Assistant && isComputerTool(p.Name) {
			return m.hub.callComputerTool(ctx, p.SessionID, p.Name, p.Args), nil
		}
		if p.Assistant && isBrowserTool(p.Name) {
			return m.hub.callBrowserTool(ctx, p.SessionID, p.Name, p.Args), nil
		}
		if p.Assistant && m.hub.isAssistantTool(ctx, p.Name) {
			return m.hub.callAssistantTool(ctx, m, p.SessionID, p.Name, p.Args), nil
		}
		return m.hub.callTool(ctx, m.cfg.ID, p.Name, p.Args), nil
	case "assistant.model":
		// Which launcher and model a message the node sends a conversation by itself runs on: the same as one from the
		// phone (assistantSendModel), so the next message from the phone finds it on them (#1554).
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		out := map[string]any{}
		if _, known := m.session(p.SessionID); !known && !m.hub.isMainConversation(ctx, m, p.SessionID) {
			return out, nil // not listed yet (the node just started): another conversation keeps its own, not 主对话's
		}
		if err := m.hub.assistantSendModel(ctx, m, p.SessionID, out); err != nil {
			return nil, err
		}
		return out, nil
	case "assistant.context":
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		return m.hub.asst().Context(ctx, assistantwire.ContextParams{SessionID: p.SessionID})
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// notifyFinished decides, after the delay, whether this end of a turn is worth a push (notify.go) and to which devices.
func (m *Machine) notifyFinished(id string, gen int) {
	ncfg := m.hub.cfg.Notify
	time.Sleep(ncfg.doneDelay())
	s, ok := m.hub.decoratedSession(m, id)
	if ok && !s.Assistant && s.Status != proto.StatusWorking {
		m.hub.kbAgentDone(m.cfg.ID, s)
	}
	if ok && s.Work != nil && s.Work.Phase == proto.WorkBlocked && (s.Work.WakeAt != nil || s.Work.InteractionID != "") {
		return // the hub goes on with it (继续, a wake), or its card already rang (work.go)
	}
	// An inbox agent's result is the draft in the inbox; a turn asked for through the bot is answered there.
	if !ok || m.hub.isInboxAgent(id) || m.hub.channelAnswered(id) {
		return
	}
	m.hub.mu.Lock()
	pushed := time.Since(m.hub.notified[id]) < 3*time.Minute // the assistant already pushed what matters (notify_user)
	m.hub.mu.Unlock()
	seen := agentState(s, m.hub.seenAt(m.cfg.ID, id)) == proto.StateIdle
	verdict, took := m.hub.turns.settle(m.cfg.ID+"/"+id, gen, s, seen, m.hub.muted(m.cfg.ID, id), pushed, time.Now(), ncfg.backgroundHold())
	if verdict != finishPush {
		return
	}
	title := s.Title
	if title == "" || title == "Claude Code" {
		title = path.Base(s.Cwd)
	}
	place := m.cfg.Name
	if s.SpaceName != "" {
		place += " · " + s.SpaceName
		if s.SlotName != "" && s.SlotName != fmt.Sprint(s.SlotIndex) {
			place += " › " + s.SlotName
		}
	}
	head, text := "✅ "+title, s.LastText
	if text == "" {
		text = "完成了"
	}
	if w := s.Work; w != nil && w.Line != "" {
		switch w.Phase {
		case proto.WorkDone:
			text = w.Line
		case proto.WorkBlocked:
			head, text = "⏳ "+title, w.Line
		}
	}
	if cutOff(text) { // the model service cut the turn off: no ✅ over the error (#1516)
		head, text = "⚠️ "+title, interruptedSummary+"："+strings.TrimSpace(text)
	}
	long := ncfg.longTurn()
	m.hub.notifyDone("done", m.cfg.ID, place, id, head, text, pushWants{Kind: "done", Took: took, Long: long})
}

// nodeMismatch: the node's jarvisd is another build than the hub's. Builds are named <commit>[-dirty]-<time>
// (deploy.sh); dev builds ("dev", empty) are never flagged. The local node is the hub binary itself.
func nodeMismatch(hub, node string) bool {
	if hub == "" || node == "" || hub == "dev" || node == "dev" {
		return false
	}
	return hub != node
}

// hasHooks: Jarvis's hooks run in the session — it sent a hook event since this hub started (m.hooked), or Jarvis
// started it (with its hooks) — so they ask for most dialogs themselves and a screen card waits for them a little
// (createScreenInteraction).
func (m *Machine) hasHooks(s proto.Session) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hooked[s.SessionID] || s.StartedByJarvis
}
