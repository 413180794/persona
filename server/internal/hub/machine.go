package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
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
	// computer: what the node last said of Cua Driver (computer.status), while the plugin cua is on for it (computer.go);
	// computerAt: when the node answered it (or, from an older node, when it was asked), so an older answer never
	// replaces it (keepComputer).
	computer   *proto.Computer
	computerAt time.Time
	// browser, browserAt: the same for ego lite (browser.status, the plugin ego; browser.go).
	browser   *proto.Browser
	browserAt time.Time
	online    bool
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
	// The web panels' open folders on this machine (fswatch.go): fswMu orders the fs.watch calls, fswSent says the
	// node was given a non-empty set, fswWarned that a failure was logged (a node without fs.watch).
	fswMu     sync.Mutex
	fswSent   bool
	fswWarned bool
	lastSeq   map[string]int // session id -> highest item seq seen
	hooked    map[string]bool
	// stopSig: the signature of the reply each session's latest Stop ended its turn with; pushedSig: the one a
	// "finished" push was already sent for, cleared when the next prompt comes (stopDuplicate).
	stopSig, pushedSig map[string]string
	// derived: a computer only a member hub has in hub.json; the center reaches it through that hub.
	derived bool

	// The manager's report (devices_link.go): managed is set once one came for this machine (or the hub made it for
	// one the manager will report); snap is the machine as it said; connected, a call would reach the node now;
	// hookedAt, the connection the hub did its part for (devices.connected); paths, its ways for the admin page.
	managed   bool
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

func newMachine(h *Hub, cfg MachineConfig) *Machine {
	return &Machine{cfg: cfg, hub: h, watchers: map[string]int{}, lastSeq: map[string]int{}}
}

func (m *Machine) snapshot() proto.Machine {
	computer := m.hub.computerOf(m) // before m.mu: it takes it
	browser := m.hub.browserOf(m)
	system := m.hub.metrics.system(m.cfg.ID)
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
	s.System, s.Computer, s.Browser = system, computer, browser
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
	if err := m.hub.callBus(context.Background(), devicesOwner, devicewire.MethodReconnect, map[string]any{"machine_id": m.cfg.ID}, nil); err != nil {
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
			return &rpc.Error{Code: proto.CodeNodeOutdated, Message: i18n.T(m.hub.requestCtxLang(ctx), "hub-machine.node_no_model_config")}
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
			go m.hub.asst().TurnStarted(context.Background(), assistantwire.SessionRef{MachineID: m.cfg.ID, SessionID: s.SessionID})
		case prev == proto.StatusWorking && (s.Status == proto.StatusDone || s.Status == proto.StatusIdle):
			go m.notifyFinished(s.SessionID, m.hub.turns.ended(key, now))
			// A new assistant conversation is named after its first turn; a task from one may be done.
			go m.hub.asst().TurnEnded(context.Background(), assistantwire.SessionRef{MachineID: m.cfg.ID, SessionID: s.SessionID})
		}
	}
	m.sessions = list
	m.lastSeen = time.Now().UTC()
	connected := m.connectedAt
	m.mu.Unlock()
	m.hub.sessionsChanged(m)
	// Messages from other conversations, for those no longer running; the todos' links and herdr marks.
	m.hub.asst().SessionsChanged(context.Background(), assistantwire.DeviceSessions{MachineID: m.cfg.ID, Sessions: list,
		Connected: connected})
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
	case "plugin.node.state", "plugin.node.event": // plugins' programs on this computer (extension_nodes.go)
		m.hub.extensionNodeEvent(m, event, params)
	case "sessions.changed":
		var p struct {
			Sessions []proto.Session `json:"sessions"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.setSessions(p.Sessions)
		}
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
		p.Items = m.hub.withInjected(p.Items)
		m.hub.itemsToSubscribers(m.cfg.ID, p.SessionID, p.Items, p.itemSpan)
		m.hub.followedItems(m.cfg.ID, p.SessionID, p.Items) // the assistant's channels follow it (assistant_channels.go)
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
	case "session.live": // a working Claude's status line as it is now: not kept, not replayed (docs/protocol.md「实时状态」)
		var p struct {
			SessionID string             `json:"session_id"`
			Live      *proto.SessionLive `json:"live"`
		}
		if json.Unmarshal(params, &p) == nil && p.SessionID != "" {
			m.hub.toSubscribersWith(m.cfg.ID, p.SessionID, proto.FeatureSessionLive,
				map[string]any{"type": "live", "machine_id": m.cfg.ID, "session_id": p.SessionID, "live": p.Live})
		}
	case "terminal.frame":
		m.hub.onTerminalFrame(m.cfg.ID, params)
	case "herdr.client.data":
		m.hub.onHerdrClientData(m.cfg.ID, params)
	case "herdr.client.end":
		m.hub.onHerdrClientEnd(m.cfg.ID, params)
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
			m.hub.followedDelta(m.cfg.ID, p.SessionID, p.Text)
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
				// A report-only hook (node/globalhooks.go) is not Persona's hooks running in the session: it asks for
				// no card, so it must not hold the screen's back (hasHooks). Its Stop still counts for stopDuplicate.
				if p["report_only"] != true {
					m.mu.Lock()
					if m.hooked == nil {
						m.hooked = map[string]bool{}
					}
					m.hooked[sid] = true
					m.mu.Unlock()
				}
				m.noteStop(sid, p)
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
		if s, ok := m.hub.extensions.assistantSummary(ctx, p.Interaction.ToolName, p.Interaction.Summary); ok {
			p.Interaction.Summary = s
		}
		return m.hub.nodeInteraction(ctx, p.Interaction)
	case "notify": // personad notify on this machine (node/scriptcli.go)
		return m.scriptNotify(params)
	case assistantwire.MethodViewCheck:
		return m.checkAssistantView(params)
	case "assistant.lark":
		// The skills' gated commands' credentials go to the assistant's machine only (its process runs them); so does
		// how to run lark-cli (its login stays on that machine, in lark-cli's own store).
		if m.cfg.ID != m.hub.assistantProfile().MachineID {
			return proto.AssistantLark{}, nil
		}
		return m.hub.assistantLark(ctx)
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
		// phone (the assistant's assistant.send_params), so the next message from the phone finds it on them (#1554).
		// One not listed yet (the node just started) that is not the main one keeps its own, not 主对话's.
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		return m.hub.asst().SendParams(ctx, m.cfg.ID, p.SessionID, map[string]any{})
	case "assistant.context":
		var p struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(params, &p)
		return m.hub.asst().Context(ctx, assistantwire.ContextParams{SessionID: p.SessionID})
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// noteStop keeps what a hook event says about completions for stopDuplicate: a Stop's reply signature, and that a
// prompt starts something new.
func (m *Machine) noteStop(sid string, p map[string]any) {
	event, _ := p["event"].(string)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch event {
	case "UserPromptSubmit":
		delete(m.pushedSig, sid)
		delete(m.stopSig, sid)
	case "Stop":
		sig, _ := p["stop_sig"].(string)
		if sig == "" {
			delete(m.stopSig, sid)
			return
		}
		if m.stopSig == nil {
			m.stopSig = map[string]string{}
		}
		m.stopSig[sid] = sig
	}
}

// stopDuplicate says whether the "finished" push about to go out repeats one already sent: the same reply (by the
// signature the node hashed from the Stop hook's last_assistant_message) with no prompt in between. That is the same
// completion seen twice, as when the status flaps working → done → working → done around one turn. A session without
// hooks, or a Stop without a reply, has no signature and is never held back. Otherwise it records the push. In
// memory: after a hub restart at worst one repeat gets through.
func (m *Machine) stopDuplicate(sid string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sig := m.stopSig[sid]
	if sig == "" {
		return false
	}
	if m.pushedSig[sid] == sig {
		return true
	}
	if m.pushedSig == nil {
		m.pushedSig = map[string]string{}
	}
	m.pushedSig[sid] = sig
	return false
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
	// An inbox agent's result is the draft in the inbox; a turn asked for through the bot is answered there; a quiet
	// task's is told in the main conversation (the assistant's taskTurnEnded, #1731).
	if !ok || m.hub.isInboxAgent(id) || m.hub.channelAnswered(id) || m.hub.quietSession(m.cfg.ID, id) {
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
	// The own conversation of a scheduled task that pushes only what its runs send, finished normally (#2396): its
	// news, if any, went out with notify_user. Decided after settle, not as a push: a turn held for its background
	// work stays followed, and the wake that ends it is judged on its own (an error there is pushed).
	if m.hub.quietTaskFinish(s) {
		return
	}
	if m.stopDuplicate(id) {
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
		text = i18n.T(m.hub.userLang(), "hub-machine.done")
	}
	if w := s.Work; w != nil && w.Line != "" {
		switch w.Phase {
		case proto.WorkDone:
			text = w.Line
		case proto.WorkBlocked:
			head, text = "⏳ "+title, w.Line
		}
	}
	// The model service cut the turn off: no ✅ over the error (#1516). A Codex turn's error is no words of its
	// answer: the session says it (#2185).
	if e := strings.TrimSpace(s.TurnError); e != "" {
		head, text = "⚠️ "+title, interruptedSummary+"："+e
	} else if cutOff(text) {
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
