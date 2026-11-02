package node

// Runtimes (docs/runtimes.md, wire in docs/protocol.md「Runtime ↔ node」): agent runtimes without a terminal that keep
// their own sessions — DSH's Web plugin first (docs/dsh.md) — connect to ~/.jarvis/runtime.sock and hang their
// sessions, status and interactions into this node. They are not hosts in the internal/host sense (where a terminal
// lives, the machine's engine): a runtime has no panes, it reports its own sessions and content, and it runs beside
// whichever engine the machine uses. Every runtime speaks the same protocol, so a new one needs no code here.
// The wire is the hub ↔ node JSON lines (internal/rpc) on a long-lived connection: the runtime pushes its session list,
// the items of watched sessions and live text; it asks the phone through interaction.create; the node sends it every
// session.* call for a session it lists. The hub and the apps see ordinary sessions and interactions.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
	"github.com/chyroc/jarvis/server/internal/transcript"
)

// RuntimeProtocol is the runtime wire's version, answered to runtime.hello; it grows only for changes old runtimes misread.
const RuntimeProtocol = 1

// RuntimeSocketPath is where runtimes reach the node.
func RuntimeSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis", "runtime.sock")
}

// runtimeCallTimeout bounds one node → runtime call; a history page of a long session is the slow one.
var runtimeCallTimeout = 30 * time.Second

// runtimeIDRe: runtime ids and the session ids a runtime may use. Wider than validID (DSH names them session-<uuid>), never a path.
var runtimeIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// runtimeHello is what a runtime says about itself first.
type runtimeHello struct {
	// Runtime is the runtime's id ("dsh"): its sessions' default agent and space kind.
	Runtime string `json:"runtime"`
	// Name is what the apps call it ("DSH"): its sessions' agent_label, the text of errors about them. Default: Runtime.
	Name string `json:"name,omitempty"`
	// Agent is the agent its sessions run, when it is not the runtime itself (a runtime hosting Claude Code).
	Agent   string `json:"agent,omitempty"`
	Version string `json:"version,omitempty"`
	// Launcher, when set, is offered in the new-session form; session.start with it goes to this runtime.
	Launcher string `json:"launcher,omitempty"`
	// LauncherModel is what the form shows next to the launcher (e.g. the runtime's default model).
	LauncherModel string `json:"launcher_model,omitempty"`
	Caps          struct {
		Start     bool `json:"start"`
		Send      bool `json:"send"`
		Interrupt bool `json:"interrupt"`
		Rename    bool `json:"rename"`
		Close     bool `json:"close"`
	} `json:"caps"`
}

// runtimeConn is one connected runtime.
type runtimeConn struct {
	serial int
	peer   *rpc.Peer

	mu       sync.Mutex
	hello    runtimeHello
	ready    bool            // runtime.hello arrived
	sessions []proto.Session // its last runtime.sessions
	watched  map[string]bool // sessions this connection was asked to watch
}

// id is the runtime's id ("" before hello).
func (h *runtimeConn) id() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hello.Runtime
}

// label is what the apps call the runtime.
func (h *runtimeConn) label() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hello.Name != "" {
		return h.hello.Name
	}
	return h.hello.Runtime
}

// runtimeRegistry is the node's registry of connected runtimes.
type runtimeRegistry struct {
	mu     sync.Mutex
	serial int
	conns  map[int]*runtimeConn
	// owner: session id -> the runtime listing it (the newest connection wins when two list the same id).
	owner map[string]*runtimeConn
	// watching: sessions the hub watches -> the last seq pushed for each, so a runtime that reconnects is asked to
	// watch them again from where the phone is.
	watching map[string]int
}

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{conns: map[int]*runtimeConn{}, owner: map[string]*runtimeConn{}, watching: map[string]int{}}
}

func (b *runtimeRegistry) add(peer *rpc.Peer) *runtimeConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.serial++
	h := &runtimeConn{serial: b.serial, peer: peer, watched: map[string]bool{}}
	b.conns[h.serial] = h
	return h
}

func (b *runtimeRegistry) remove(h *runtimeConn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.conns, h.serial)
	for id, o := range b.owner {
		if o == h {
			delete(b.owner, id)
		}
	}
}

// setSessions records a runtime's list and returns the watched sessions this connection has not been asked to watch
// yet (with the seq to start after).
func (b *runtimeRegistry) setSessions(h *runtimeConn, list []proto.Session) map[string]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, o := range b.owner {
		if o == h {
			delete(b.owner, id)
		}
	}
	rewatch := map[string]int{}
	h.mu.Lock()
	h.sessions = list
	for _, s := range list {
		b.owner[s.SessionID] = h
		if since, ok := b.watching[s.SessionID]; ok && !h.watched[s.SessionID] {
			h.watched[s.SessionID] = true
			rewatch[s.SessionID] = since
		}
	}
	h.mu.Unlock()
	return rewatch
}

func (b *runtimeRegistry) ownerOf(id string) *runtimeConn {
	if b == nil || id == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.owner[id]
}

// sessions lists every connected runtime's sessions, each id once.
func (b *runtimeRegistry) sessions() []proto.Session {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	conns := make([]*runtimeConn, 0, len(b.conns))
	for _, h := range b.conns {
		conns = append(conns, h)
	}
	owner := make(map[string]*runtimeConn, len(b.owner))
	for id, h := range b.owner {
		owner[id] = h
	}
	b.mu.Unlock()
	sort.Slice(conns, func(i, j int) bool { return conns[i].serial < conns[j].serial })
	var out []proto.Session
	for _, h := range conns {
		h.mu.Lock()
		for _, s := range h.sessions {
			if owner[s.SessionID] == h {
				out = append(out, s)
			}
		}
		h.mu.Unlock()
	}
	return out
}

// launchers are the runtimes' entries for the new-session form.
func (b *runtimeRegistry) launchers() []proto.Launcher {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []proto.Launcher
	seen := map[string]bool{}
	for _, h := range b.conns {
		h.mu.Lock()
		if h.ready && h.hello.Caps.Start && h.hello.Launcher != "" && !seen[h.hello.Launcher] {
			seen[h.hello.Launcher] = true
			// Agent, as for a Codex launcher (proto.Launcher.Agent): what it starts is not Claude Code, so the apps do
			// not offer it where only Claude Code or Codex can run (the Jarvis assistant).
			agent := h.hello.Agent
			if agent == "" {
				agent = h.hello.Runtime
			}
			if agent == "claude" {
				agent = ""
			}
			out = append(out, proto.Launcher{Name: h.hello.Launcher, Model: h.hello.LauncherModel, Source: h.hello.Runtime, Agent: agent})
		}
		h.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// launcherRuntime is the runtime offering a launcher name, nil when none does.
func (b *runtimeRegistry) launcherRuntime(name string) *runtimeConn {
	if b == nil || name == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.conns {
		h.mu.Lock()
		match := h.ready && h.hello.Caps.Start && h.hello.Launcher == name
		h.mu.Unlock()
		if match {
			return h
		}
	}
	return nil
}

func (b *runtimeRegistry) watch(h *runtimeConn, id string, since int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.watching[id]; !ok || since > cur {
		b.watching[id] = since
	}
	h.mu.Lock()
	h.watched[id] = true
	h.mu.Unlock()
}

func (b *runtimeRegistry) unwatch(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.watching, id)
	for _, h := range b.conns {
		h.mu.Lock()
		delete(h.watched, id)
		h.mu.Unlock()
	}
}

// pushed moves a watched session's seq forward (the items a runtime pushed reached the hub).
func (b *runtimeRegistry) pushed(id string, seq int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.watching[id]; ok && seq > cur {
		b.watching[id] = seq
	}
}

// listenRuntimeSocket serves runtimes on runtime.sock until ctx ends, re-creating the socket file if something
// removes it (an older node shutting down), as the hook socket does.
func (n *Node) listenRuntimeSocket(ctx context.Context) (net.Listener, error) {
	ln, err := n.listenRuntimeOnce(ctx)
	if err != nil {
		return nil, err
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if _, err := os.Stat(n.runtimePath()); os.IsNotExist(err) {
				n.logger.Printf("runtime socket vanished, re-creating")
				if _, err := n.listenRuntimeOnce(ctx); err != nil {
					n.logger.Printf("runtime socket: %v", err)
				}
			}
		}
	}()
	return ln, nil
}

func (n *Node) runtimePath() string {
	if n.runtimeSock != "" {
		return n.runtimeSock
	}
	return RuntimeSocketPath()
}

func (n *Node) listenRuntimeOnce(ctx context.Context) (net.Listener, error) {
	path := n.runtimePath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false) // by then the path may belong to a newer node
	_ = os.Chmod(path, 0o600)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go n.serveRuntime(ctx, c)
		}
	}()
	return ln, nil
}

// serveRuntime runs one runtime's connection until it ends; its sessions leave the list with it.
func (n *Node) serveRuntime(ctx context.Context, c net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	defer c.Close()
	peer := rpc.New("r", c)
	h := n.runtimes.add(peer)
	peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return n.runtimeRequest(ctx, h, method, params)
	}
	peer.OnEvent = func(event string, params json.RawMessage) { n.runtimeEvent(h, event, params) }
	err := peer.Serve(ctx, c)
	peer.Close(err)
	n.runtimes.remove(h)
	if id := h.id(); id != "" {
		n.logger.Printf("runtime %s #%d left: %v", id, h.serial, err)
	}
	n.notifySessions()
}

// runtimeRequest answers a runtime's call.
func (n *Node) runtimeRequest(ctx context.Context, h *runtimeConn, method string, params json.RawMessage) (any, error) {
	switch method {
	case "runtime.hello":
		var hello runtimeHello
		if err := json.Unmarshal(params, &hello); err != nil {
			return nil, fmt.Errorf("bad runtime.hello: %w", err)
		}
		hello.Runtime = strings.TrimSpace(hello.Runtime)
		if hello.Runtime == "" || !runtimeIDRe.MatchString(hello.Runtime) {
			return nil, errors.New("runtime.hello needs a runtime id ([A-Za-z0-9][A-Za-z0-9_.-]*)")
		}
		hello.Name = strings.TrimSpace(hello.Name)
		if hello.Agent = strings.TrimSpace(hello.Agent); hello.Agent != "" && !runtimeIDRe.MatchString(hello.Agent) {
			return nil, fmt.Errorf("runtime.hello: bad agent %q", hello.Agent)
		}
		h.mu.Lock()
		h.hello, h.ready = hello, true
		h.mu.Unlock()
		n.logger.Printf("runtime %s %s #%d connected", hello.Runtime, hello.Version, h.serial)
		return map[string]any{"protocol": RuntimeProtocol, "node_version": n.Version}, nil
	case "interaction.create":
		return n.runtimeAsk(ctx, h, params)
	case "mcp.tools":
		if n.peer == nil {
			return map[string]any{"tools": []proto.Tool{}}, nil
		}
		var res json.RawMessage
		err := n.peer.Call(ctx, "mcp.tools", map[string]any{}, &res)
		return res, err
	case "mcp.call":
		if n.peer == nil {
			return proto.TextResult("Jarvis hub is not connected", true), nil
		}
		var res json.RawMessage
		err := n.peer.Call(ctx, "mcp.call", json.RawMessage(params), &res)
		return res, err
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// runtimeEvent takes a runtime's push.
func (n *Node) runtimeEvent(h *runtimeConn, event string, params json.RawMessage) {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready {
		return // hello first
	}
	switch event {
	case "runtime.sessions":
		var p struct {
			Sessions []proto.Session `json:"sessions"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		list := n.normalizeRuntimeSessions(h, p.Sessions)
		for id, since := range n.runtimes.setSessions(h, list) {
			go n.runtimeWatch(h, id, since)
		}
		n.publish(n.sessionList(context.Background(), false))
	case "session.items":
		var p struct {
			SessionID string       `json:"session_id"`
			Items     []proto.Item `json:"items"`
			ToSeq     *int         `json:"to_seq"`
		}
		if json.Unmarshal(params, &p) != nil || n.runtimes.ownerOf(p.SessionID) != h {
			return
		}
		last := 0
		if len(p.Items) > 0 {
			last = p.Items[len(p.Items)-1].Seq
		}
		if p.ToSeq != nil {
			last = max(last, *p.ToSeq)
		}
		n.runtimes.pushed(p.SessionID, last)
		if n.peer != nil {
			_ = n.peer.Notify("session.items", params)
		}
	case "session.delta":
		var p struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(params, &p) != nil || n.runtimes.ownerOf(p.SessionID) != h || n.peer == nil {
			return
		}
		_ = n.peer.Notify("session.delta", params)
	case "interaction.cancel":
		if n.peer != nil {
			_ = n.peer.Notify("interaction.cancel", params)
		}
	}
}

// normalizeRuntimeSessions keeps the sessions a runtime may list (valid ids, once each) and fills what the apps expect:
// the agent and its label, the space kind (the runtime's id).
func (n *Node) normalizeRuntimeSessions(h *runtimeConn, list []proto.Session) []proto.Session {
	h.mu.Lock()
	rt, agent, label, caps := h.hello.Runtime, h.hello.Agent, h.hello.Name, h.hello.Caps
	h.mu.Unlock()
	if agent == "" {
		agent = rt
	}
	if label == "" {
		label = rt
	}
	readOnly := ""
	if !caps.Send {
		readOnly = label + " 没有开放从手机发消息"
	}
	seen := map[string]bool{}
	out := make([]proto.Session, 0, len(list))
	for _, s := range list {
		if !runtimeIDRe.MatchString(s.SessionID) || seen[s.SessionID] {
			continue
		}
		seen[s.SessionID] = true
		s.MachineID, s.State, s.PendingInteractions = "", "", 0 // the hub's to fill
		s.StartedByJarvis, s.Assistant = false, false
		if s.Agent == "" {
			s.Agent = agent
		}
		if s.AgentLabel == "" && s.Agent == rt {
			s.AgentLabel = label // the runtime runs its own agent (DSH): the apps call it by the runtime's name
		}
		if s.SpaceKind == "" {
			s.SpaceKind = rt
		}
		// The source fields Codex app-server sessions use too (docs/protocol.md「Codex 来源与能力」): the apps gate
		// sending, interrupting, answering cards and every terminal control on capabilities, the hub guards sends.
		// What a runtime declared, never more: no terminal, no attach, cards always answerable (they are its own asks).
		s.SourceKind, s.SourceInstanceID, s.SourceThreadID, s.ParentThreadID = proto.SourceRuntime, rt, "", ""
		s.Capabilities = &proto.SessionCapabilities{Read: true, Send: caps.Send, Interrupt: caps.Interrupt, Approve: true}
		s.ReadOnlyReason, s.Attached, s.CodexRoute, s.CodexRouteNote = readOnly, false, "", ""
		s.PaneID, s.TabID, s.WorkspaceID = "", "", ""
		switch s.Status {
		case proto.StatusWorking, proto.StatusBlocked, proto.StatusDone, proto.StatusIdle, proto.StatusOffline:
		default:
			s.Status = proto.StatusUnknown
		}
		if s.Title == "" && s.Cwd != "" {
			s.Title = filepath.Base(s.Cwd)
		}
		if s.UpdatedAt.IsZero() {
			s.UpdatedAt = time.Now().UTC()
		}
		s.UpdatedAt = s.UpdatedAt.UTC()
		out = append(out, s)
	}
	return out
}

// runtimeWatch asks a runtime to push a session's new items, from since on.
func (n *Node) runtimeWatch(h *runtimeConn, id string, since int) {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeCallTimeout)
	defer cancel()
	if err := h.peer.Call(ctx, "session.watch", map[string]any{"session_id": id, "since_seq": since}, nil); err != nil {
		n.logger.Printf("runtime %s: watch %s: %v", h.id(), id, err)
	}
}

// runtimeAsk forwards a runtime's question / plan / permission to the phone and returns the answer; {dismiss: true}
// when nobody answered (expired, handed back to the computer, no hub). A node going away under a resumable
// interaction answers with code node_gone: the runtime asks the next node with the same id.
func (n *Node) runtimeAsk(ctx context.Context, h *runtimeConn, params json.RawMessage) (any, error) {
	var p struct {
		Interaction proto.Interaction `json:"interaction"`
		// TimeoutSeconds: how long the runtime waits at most (default: the hook timeout).
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("bad interaction: %w", err)
	}
	ix := p.Interaction
	switch ix.Kind {
	case proto.IxQuestion, proto.IxPlan, proto.IxPermission:
	default:
		return nil, fmt.Errorf("unsupported interaction kind %q", ix.Kind)
	}
	if n.runtimes.ownerOf(ix.SessionID) != h {
		return nil, fmt.Errorf("session %s is not listed by this runtime", ix.SessionID)
	}
	rt := h.id()
	if ix.ID == "" || !runtimeIDRe.MatchString(ix.ID) {
		key := ix.ToolUseID
		if key == "" {
			key = newUUID()
		}
		sum := sha256.Sum256([]byte(rt + "\x00" + ix.SessionID + "\x00" + ix.Kind + "\x00" + key))
		ix.ID = "ix_" + hex.EncodeToString(sum[:])[:16]
	}
	// Who asks, as on a Codex card (proto.Interaction.Agent): the apps draw a runtime's card as its own agent's
	// (同意 / 拒绝, its reason, 总是同意 only with an always_label it gives), not as Claude's, whose 本会话总是允许 a
	// runtime cannot keep. A runtime hosting Claude Code (agent "claude") gets Claude's card.
	h.mu.Lock()
	agent, label := h.hello.Agent, h.hello.Name
	h.mu.Unlock()
	if agent == "" {
		agent = rt
	}
	if label == "" {
		label = rt
	}
	if agent == "claude" {
		ix.Agent, ix.AgentLabel = "", ""
	} else {
		ix.Agent, ix.AgentLabel = agent, label
	}
	now := time.Now().UTC()
	ix.MachineID, ix.Screen, ix.Dialog, ix.Secret = "", "", nil, false
	ix.CreatedAt = now
	timeout := HookTimeout
	if p.TimeoutSeconds > 0 && time.Duration(p.TimeoutSeconds)*time.Second < timeout {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	ix.ExpiresAt = now.Add(timeout)
	ix.Resumable = ix.ToolUseID != "" // its id is the same when the runtime asks again through the next node
	for _, s := range h.sessionsCopy() {
		if s.SessionID == ix.SessionID {
			if ix.SessionTitle == "" {
				ix.SessionTitle = s.Title
			}
			if ix.Cwd == "" {
				ix.Cwd = s.Cwd
			}
		}
	}
	res, ok, err := n.ask(ctx, ix)
	if errors.Is(err, errNodeGone) {
		return nil, &rpc.Error{Message: "node going away", Code: "node_gone"}
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return proto.Resolution{Dismiss: true}, nil
	}
	return res, nil
}

func (h *runtimeConn) sessionsCopy() []proto.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]proto.Session(nil), h.sessions...)
}

// errRuntimeUnsupported: a session.* call the session's runtime has nothing for (no terminal, no TUI modes, a cap it
// did not declare). Like a terminal host lacking a capability (host.ErrUnsupported), the text starts with
// "unsupported": the hub reads that prefix (a terminal stream it cannot open falls back to reading the screen).
func errRuntimeUnsupported(label, what string) error {
	return &rpc.Error{Message: fmt.Sprintf("unsupported: %s 的会话不支持%s", label, what), Code: "unsupported"}
}

// runtimeHandle serves a hub call for a session a runtime lists; handled is false for methods that are not about one
// session (the node answers those itself).
func (n *Node) runtimeHandle(ctx context.Context, h *runtimeConn, method string, params json.RawMessage) (res any, err error, handled bool) {
	label := h.label()
	h.mu.Lock()
	caps := h.hello.Caps
	h.mu.Unlock()
	call := func(m string, p any) (json.RawMessage, error) {
		ctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
		defer cancel()
		var out json.RawMessage
		err := h.peer.Call(ctx, m, p, &out)
		if err != nil && !isRPCError(err) {
			err = fmt.Errorf("%s: %w", label, err)
		}
		return out, err
	}
	var p struct {
		SessionID  string          `json:"session_id"`
		Text       string          `json:"text"`
		Images     []proto.Image   `json:"images"`
		SinceSeq   int             `json:"since_seq"`
		FoldWork   bool            `json:"fold_work"`
		LazyImages bool            `json:"lazy_images"`
		AfterSeq   *int            `json:"after_seq"`
		ThroughSeq int             `json:"through_seq"`
		Title      *string         `json:"title"`
		ItemID     string          `json:"item_id"`
		Raw        json.RawMessage `json:"-"`
	}
	_ = json.Unmarshal(params, &p)
	switch method {
	case "session.items":
		out, err := call("session.items", params)
		if err != nil {
			return nil, err, true
		}
		if !p.FoldWork || p.AfterSeq != nil || p.ThroughSeq > 0 {
			return out, nil, true
		}
		// Pages counted back from the end fold the agent's work, as for transcripts (transcript.FoldWork).
		var page map[string]json.RawMessage
		if json.Unmarshal(out, &page) != nil {
			return out, nil, true
		}
		var items []proto.Item
		if json.Unmarshal(page["items"], &items) != nil {
			return out, nil, true
		}
		folded, _ := json.Marshal(nonNil(transcript.FoldWork(items)))
		page["items"] = folded
		return page, nil, true
	case "session.image", "session.item":
		out, err := call(method, params)
		return out, err, true
	case "session.watch":
		n.runtimes.watch(h, p.SessionID, p.SinceSeq)
		_, err := call("session.watch", map[string]any{"session_id": p.SessionID, "since_seq": p.SinceSeq})
		return okResult(err), err, true
	case "session.unwatch":
		n.runtimes.unwatch(p.SessionID)
		_, err := call("session.unwatch", map[string]any{"session_id": p.SessionID})
		return okResult(err), err, true
	case "session.send":
		if !caps.Send {
			return nil, errRuntimeUnsupported(label, "从手机发消息"), true
		}
		var opts sendOpts
		_ = json.Unmarshal(params, &opts)
		err := n.sends.once(opts.ClientMsgID, func() error {
			_, err := call("session.send", map[string]any{"session_id": p.SessionID, "text": p.Text, "images": p.Images,
				"client_msg_id": opts.ClientMsgID})
			return err
		})
		return okResult(err), err, true
	case "session.interrupt":
		if !caps.Interrupt {
			return nil, errRuntimeUnsupported(label, "中断"), true
		}
		_, err := call("session.interrupt", map[string]any{"session_id": p.SessionID})
		return okResult(err), err, true
	case "session.close":
		if !caps.Close {
			return nil, errRuntimeUnsupported(label, "从手机关闭"), true
		}
		_, err := call("session.close", map[string]any{"session_id": p.SessionID})
		return okResult(err), err, true
	case "session.rename":
		if p.Title == nil || strings.TrimSpace(*p.Title) == "" {
			return okResult(nil), nil, true // a tab name: these sessions have no tab
		}
		if !caps.Rename {
			return nil, errRuntimeUnsupported(label, "改名"), true
		}
		_, err := call("session.rename", map[string]any{"session_id": p.SessionID, "title": strings.TrimSpace(*p.Title)})
		return okResult(err), err, true
	case "session.screen":
		return map[string]any{"text": "", "status": n.runtimeStatus(h, p.SessionID)}, nil, true
	case "session.info":
		for _, s := range h.sessionsCopy() {
			if s.SessionID == p.SessionID {
				return map[string]any{"session_id": s.SessionID, "cwd": s.Cwd, "model": s.Model, "agent": s.Agent,
					"last_activity": s.UpdatedAt}, nil, true
			}
		}
		return nil, fmt.Errorf("session %s is not listed", p.SessionID), true
	case "session.keys", "session.screen.before", "terminal.watch":
		return nil, errRuntimeUnsupported(label, "终端"), true
	case "terminal.unwatch":
		return okResult(nil), nil, true
	case "session.modes", "session.queue.edit":
		return nil, errRuntimeUnsupported(label, "这个操作"), true
	}
	return nil, nil, false
}

func (n *Node) runtimeStatus(h *runtimeConn, id string) string {
	for _, s := range h.sessionsCopy() {
		if s.SessionID == id {
			return s.Status
		}
	}
	return proto.StatusUnknown
}

// runtimeStart starts a session in the runtime offering launcher; nil when no runtime does.
func (n *Node) runtimeStart(ctx context.Context, launcher, cwd, prompt string) (*proto.Session, error, bool) {
	h := n.runtimes.launcherRuntime(launcher)
	if h == nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
	defer cancel()
	var res struct {
		Session proto.Session `json:"session"`
	}
	if err := h.peer.Call(ctx, "session.start", map[string]any{"cwd": cwd, "prompt": prompt}, &res); err != nil {
		return nil, err, true
	}
	list := n.normalizeRuntimeSessions(h, []proto.Session{res.Session})
	if len(list) == 0 {
		return nil, fmt.Errorf("%s started a session without a valid id", h.label()), true
	}
	s := list[0]
	// Listed right away, before the runtime's next runtime.sessions, so the chat that opens next finds it.
	n.runtimes.mu.Lock()
	n.runtimes.owner[s.SessionID] = h
	n.runtimes.mu.Unlock()
	h.mu.Lock()
	found := false
	for i := range h.sessions {
		if h.sessions[i].SessionID == s.SessionID {
			h.sessions[i], found = s, true
		}
	}
	if !found {
		h.sessions = append(h.sessions, s)
	}
	h.mu.Unlock()
	n.publish(n.sessionList(ctx, false))
	return &s, nil, true
}

func okResult(err error) any {
	if err != nil {
		return nil
	}
	return map[string]any{"ok": true}
}

func isRPCError(err error) bool {
	var e *rpc.Error
	return errors.As(err, &e)
}
