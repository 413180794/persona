package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

var errOffline = errors.New("machine is offline")

// Machine is the hub's connection to one node (local subprocess or `ssh host jarvisd node --stdio`).
type Machine struct {
	cfg MachineConfig
	hub *Hub

	mu       sync.Mutex
	peer     *rpc.Peer
	info     proto.NodeInfo
	online   bool
	err      string
	lastSeen time.Time
	sessions []proto.Session
	watchers map[string]int // session id -> subscribed clients
	lastSeq  map[string]int // session id -> highest item seq seen
	kick     chan struct{}
}

func newMachine(h *Hub, cfg MachineConfig) *Machine {
	return &Machine{cfg: cfg, hub: h, watchers: map[string]int{}, lastSeq: map[string]int{}, kick: make(chan struct{}, 1)}
}

func (m *Machine) snapshot() proto.Machine {
	m.mu.Lock()
	defer m.mu.Unlock()
	icon := m.cfg.Icon
	if icon == "" {
		icon = "laptop"
	}
	return proto.Machine{
		ID: m.cfg.ID, Name: m.cfg.Name, Kind: m.cfg.Kind, Icon: icon, SSHHost: m.cfg.SSHHost, Online: m.online, Error: m.err,
		Hostname: m.info.Hostname, OS: m.info.OS, Arch: m.info.Arch, Home: m.info.Home,
		HerdrVersion: m.info.HerdrVersion, ClaudeVersion: m.info.ClaudeVersion, LastSeen: m.lastSeen,
		NodeVersion: m.info.Version, NodeMismatch: m.online && nodeMismatch(m.hub.version, m.info.Version),
	}
}

// run keeps the node connection alive with exponential backoff.
func (m *Machine) run(ctx context.Context) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := m.connect(ctx)
		m.setOffline(err)
		if time.Since(start) > time.Minute {
			backoff = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		case <-m.kick:
			backoff = 2 * time.Second
		}
		backoff = min(backoff*2, 60*time.Second)
	}
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
	return exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", host, bin+" node --stdio")
}

func (m *Machine) connect(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := m.command(ctx)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Wait() }()
	defer cancel()

	var lastErr strings.Builder
	var errMu sync.Mutex
	go func() {
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

	peer := rpc.New("h", stdin)
	peer.OnEvent = m.onEvent
	peer.OnRequest = m.onRequest
	served := make(chan error, 1)
	go func() { served <- peer.Serve(ctx, stdout) }()

	ictx, icancel := context.WithTimeout(ctx, 40*time.Second)
	var info proto.NodeInfo
	err = peer.Call(ictx, "node.info", map[string]any{}, &info)
	icancel()
	if err != nil {
		peer.Close(err)
		errMu.Lock()
		msg := lastErr.String()
		errMu.Unlock()
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}

	m.mu.Lock()
	m.peer, m.info, m.online, m.err, m.lastSeen = peer, info, true, "", time.Now().UTC()
	rewatch := map[string]int{}
	for id, n := range m.watchers {
		if n > 0 {
			rewatch[id] = m.lastSeq[id]
		}
	}
	m.mu.Unlock()
	m.hub.logf("[%s] connected: %s %s/%s herdr %s claude %s", m.cfg.ID, info.Hostname, info.OS, info.Arch, info.HerdrVersion, info.ClaudeVersion)
	if nodeMismatch(m.hub.version, info.Version) {
		m.hub.logf("[%s] node %s is not the hub's build %s: deploy it (scripts/deploy.sh %s)", m.cfg.ID, info.Version, m.hub.version, m.cfg.ID)
	}
	m.hub.broadcastMachines()

	m.configureNode(ctx, peer)
	var list struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if err := peer.Call(ctx, "sessions.list", map[string]any{}, &list); err == nil {
		m.setSessions(list.Sessions)
	}
	for id, since := range rewatch {
		_ = peer.Call(ctx, "session.watch", map[string]any{"session_id": id, "since_seq": since}, nil)
	}

	select {
	case err = <-served:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		errMu.Lock()
		if msg := lastErr.String(); msg != "" {
			err = fmt.Errorf("node exited: %s", msg)
		} else {
			err = errors.New("node exited")
		}
		errMu.Unlock()
	}
	return err
}

func (m *Machine) setOffline(err error) {
	m.mu.Lock()
	m.peer = nil
	m.online = false
	if err != nil {
		m.err = err.Error()
	}
	for i := range m.sessions {
		if m.sessions[i].Status != proto.StatusOffline {
			m.sessions[i].Status = proto.StatusUnknown
		}
	}
	m.mu.Unlock()
	m.hub.logf("[%s] offline: %v", m.cfg.ID, err)
	m.hub.broadcastMachines()
	m.hub.cancelMachineInteractions(m.cfg.ID)
}

func (m *Machine) call(ctx context.Context, method string, params any, out any) error {
	m.mu.Lock()
	p := m.peer
	m.mu.Unlock()
	if p == nil {
		return fmt.Errorf("%s: %w", m.cfg.Name, errOffline)
	}
	return p.Call(ctx, method, params, out)
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
		// Any agent (herdr or headless, started by Jarvis or not) finishing a turn may notify the phone (notify.go).
		prev, key := before[s.SessionID], m.cfg.ID+"/"+s.SessionID
		switch {
		case s.Status == proto.StatusWorking && prev != proto.StatusWorking:
			m.hub.turns.working(key, now)
		case prev == proto.StatusWorking && (s.Status == proto.StatusDone || s.Status == proto.StatusIdle):
			go m.notifyFinished(s.SessionID, m.hub.turns.ended(key, now))
		}
	}
	m.sessions = list
	m.lastSeen = time.Now().UTC()
	m.mu.Unlock()
	m.hub.sessionsChanged(m)
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
	case "session.items":
		var p struct {
			SessionID string       `json:"session_id"`
			Items     []proto.Item `json:"items"`
		}
		if json.Unmarshal(params, &p) != nil || len(p.Items) == 0 {
			return
		}
		m.mu.Lock()
		if last := p.Items[len(p.Items)-1].Seq; last > m.lastSeq[p.SessionID] {
			m.lastSeq[p.SessionID] = last
		}
		m.mu.Unlock()
		m.hub.itemsToSubscribers(m.cfg.ID, p.SessionID, p.Items)
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
	case "session.delta":
		var p struct {
			SessionID string `json:"session_id"`
			Text      string `json:"text"`
		}
		if json.Unmarshal(params, &p) == nil {
			m.hub.toSubscribers(m.cfg.ID, p.SessionID, map[string]any{"type": "delta", "machine_id": m.cfg.ID, "session_id": p.SessionID, "text": p.Text})
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
	case "claude.dirs":
		m.rememberClaudeDirs(params)
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
		return m.hub.waitInteraction(ctx, p.Interaction)
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
		if p.Assistant && isAssistantTool(p.Name) {
			return m.hub.callAssistantTool(ctx, m, p.SessionID, p.Name, p.Args), nil
		}
		return m.hub.callTool(ctx, m.cfg.ID, p.Name, p.Args), nil
	case "assistant.context":
		return map[string]any{"text": m.hub.assistantSnapshot(ctx)}, nil
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// notifyFinished decides, after the delay, whether this end of a turn is worth a push (notify.go) and to which devices.
func (m *Machine) notifyFinished(id string, gen int) {
	ncfg := m.hub.cfg.Notify
	time.Sleep(ncfg.doneDelay())
	s, ok := m.session(id)
	if !ok {
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
	text := s.LastText
	if text == "" {
		text = "完成了"
	}
	long := ncfg.longTurn()
	m.hub.push.notifyDone(m.cfg.ID, place, id, "✅ "+title, text, func(p NotifyPrefs) bool { return p.wantsDone(took, long) })
}

// nodeMismatch: the node's jarvisd is another build than the hub's. Builds are named <commit>[-dirty]-<time>
// (deploy.sh); dev builds ("dev", empty) are never flagged. The local node is the hub binary itself.
func nodeMismatch(hub, node string) bool {
	if hub == "" || node == "" || hub == "dev" || node == "dev" {
		return false
	}
	return hub != node
}
