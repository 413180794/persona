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

// agent is the agent its sessions run.
func (h *runtimeConn) agent() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hello.Agent != "" {
		return h.hello.Agent
	}
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
	// known: session id -> the runtime that listed it or said it has it, kept after it leaves the list. A runtime
	// lists only its recent sessions (DSH: the latest 80 of thousands), but an older one is still its own: calls
	// about it go to that runtime, never to the terminal host as a Claude session to resume (that sent a DSH
	// session's message to herdr, on a machine without herdr).
	known map[string]knownSession
	// unclaimed: ids no connected runtime had when asked, and until when not to ask again.
	unclaimed map[string]time.Time
}

// knownSession is the runtime a session belongs to, by the runtime's id (a reconnect is the same runtime).
type knownSession struct {
	runtime, label string
	at             time.Time
}

// maxKnown bounds the remembered sessions; the oldest go first.
const maxKnown = 8192

func newRuntimeRegistry() *runtimeRegistry {
	return &runtimeRegistry{conns: map[int]*runtimeConn{}, owner: map[string]*runtimeConn{}, watching: map[string]int{},
		known: map[string]knownSession{}, unclaimed: map[string]time.Time{}}
}

// remember records that id belongs to h's runtime. Callers hold b.mu and h.mu.
func (b *runtimeRegistry) remember(h *runtimeConn, id string, now time.Time) {
	label := h.hello.Name
	if label == "" {
		label = h.hello.Runtime
	}
	b.known[id] = knownSession{runtime: h.hello.Runtime, label: label, at: now}
	delete(b.unclaimed, id)
	if len(b.known) > maxKnown {
		oldest := make([]string, 0, len(b.known))
		for k := range b.known {
			oldest = append(oldest, k)
		}
		sort.Slice(oldest, func(i, j int) bool { return b.known[oldest[i]].at.Before(b.known[oldest[j]].at) })
		for _, k := range oldest[:len(oldest)-maxKnown*3/4] {
			delete(b.known, k)
		}
	}
}

// byRuntime is the newest ready connection of the runtime with this id, nil when it is not connected. Callers hold b.mu.
func (b *runtimeRegistry) byRuntime(rt string) *runtimeConn {
	var best *runtimeConn
	for _, h := range b.conns {
		h.mu.Lock()
		ok := h.ready && h.hello.Runtime == rt
		h.mu.Unlock()
		if ok && (best == nil || h.serial > best.serial) {
			best = h
		}
	}
	return best
}

// belongs is the runtime a session belongs to: the connection listing it, else the connected runtime that listed it
// before; known: it belongs to a runtime (connected or not), with that runtime's name.
func (b *runtimeRegistry) belongs(id string) (h *runtimeConn, label string, known bool) {
	if b == nil || id == "" {
		return nil, "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.owner[id]; o != nil {
		return o, "", true
	}
	k, ok := b.known[id]
	if !ok {
		return nil, "", false
	}
	return b.byRuntime(k.runtime), k.label, true
}

// owns: the session's calls and pushes are h's (it lists the session, or listed or claimed it before).
func (b *runtimeRegistry) owns(h *runtimeConn, id string) bool {
	got, _, _ := b.belongs(id)
	return got != nil && got == h
}

// claim records that h has the session (it answered for it).
func (b *runtimeRegistry) claim(h *runtimeConn, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h.mu.Lock()
	b.remember(h, id, time.Now())
	h.mu.Unlock()
}

// askable are the connections to ask about a session nobody claimed yet, newest first; none while the id was asked
// about lately.
func (b *runtimeRegistry) askable(id string) []*runtimeConn {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if until, ok := b.unclaimed[id]; ok && time.Now().Before(until) {
		return nil
	}
	var out []*runtimeConn
	for _, h := range b.conns {
		h.mu.Lock()
		if h.ready {
			out = append(out, h)
		}
		h.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].serial > out[j].serial })
	return out
}

// unclaim: no runtime had the session; it is not asked about again for a minute.
func (b *runtimeRegistry) unclaim(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, until := range b.unclaimed {
		if time.Now().After(until) {
			delete(b.unclaimed, k)
		}
	}
	b.unclaimed[id] = time.Now().Add(time.Minute)
}

// ready are the connections that said hello.
func (b *runtimeRegistry) ready() []*runtimeConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*runtimeConn
	for _, h := range b.conns {
		h.mu.Lock()
		if h.ready {
			out = append(out, h)
		}
		h.mu.Unlock()
	}
	return out
}

// resetWatches forgets what the hub watched (another hub connection, or none, from now on). Runtimes are not asked to
// stop: an unwatch could reach one after the next hub has watched the session again, and what they push meanwhile is
// harmless (the hub drops items nobody subscribed to).
func (b *runtimeRegistry) resetWatches() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.watching = map[string]int{}
	for _, h := range b.conns {
		h.mu.Lock()
		h.watched = map[string]bool{}
		h.mu.Unlock()
	}
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
	now := time.Now()
	h.mu.Lock()
	h.sessions = list
	for _, s := range list {
		b.owner[s.SessionID] = h
		b.remember(h, s.SessionID, now)
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

// listenRuntimeSocket gives n a runtime socket of its own, at runtimePath, for the connection it serves itself
// (`--stdio` with no resident node: runtimehost.go). A resident node's Nodes share one instead.
func (n *Node) listenRuntimeSocket(ctx context.Context) (*RuntimeHost, error) {
	rh := &RuntimeHost{reg: n.runtimes, path: n.runtimePath(), version: n.Version, logger: n.logger}
	if rh.reg == nil {
		rh.reg = newRuntimeRegistry()
	}
	if err := rh.Listen(ctx); err != nil {
		return nil, err
	}
	rh.attach(n)
	return rh, nil
}

func (n *Node) runtimePath() string {
	if n.runtimeSock != "" {
		return n.runtimeSock
	}
	return RuntimeSocketPath()
}

// runtimeEvent takes a runtime's push for the hub (its list is the host's: RuntimeHost.event).
func (n *Node) runtimeEvent(h *runtimeConn, event string, params json.RawMessage) {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready {
		return // hello first
	}
	switch event {
	case "session.items":
		var p struct {
			SessionID string       `json:"session_id"`
			Items     []proto.Item `json:"items"`
			ToSeq     *int         `json:"to_seq"`
		}
		if json.Unmarshal(params, &p) != nil || !n.runtimes.owns(h, p.SessionID) {
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
		if json.Unmarshal(params, &p) != nil || !n.runtimes.owns(h, p.SessionID) || n.peer == nil {
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
func normalizeRuntimeSessions(h *runtimeConn, list []proto.Session) []proto.Session {
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
	if !n.runtimes.owns(h, ix.SessionID) {
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

// claimMethods are the calls that ask the connected runtimes about a session nobody claimed yet: what the apps do
// with a session they show (open it, page it, send to it, stop it).
var claimMethods = map[string]bool{"session.send": true, "session.items": true, "session.item": true, "session.image": true,
	"session.watch": true, "session.info": true, "session.interrupt": true}

// runtimeClaimTimeout bounds asking one runtime whether it has a session.
var runtimeClaimTimeout = 5 * time.Second

// runtimeFor is the runtime a hub call about session id goes to: the one listing it, else the one that listed it
// before (an older session it no longer lists), else — for a session this machine has nothing else for (no agent, no
// transcript) — the first connected runtime that answers for it. A session of a runtime that is not connected now is
// refused (errRuntimeAway): it is not a Claude session to resume in a terminal.
func (n *Node) runtimeFor(ctx context.Context, method, id string) (*runtimeConn, error) {
	if id == "" || !strings.HasPrefix(method, "session.") || method == "session.start" {
		return nil, nil
	}
	h, label, known := n.runtimes.belongs(id)
	if h != nil {
		return h, nil
	}
	if known {
		if claimMethods[method] || method == "session.rename" || method == "session.close" {
			return nil, errRuntimeAway(label)
		}
		return nil, nil // unwatching and the like: nothing to refuse
	}
	if !claimMethods[method] || !runtimeIDRe.MatchString(id) {
		return nil, nil
	}
	conns := n.runtimes.askable(id)
	if len(conns) == 0 || n.hasOwnSession(id) {
		return nil, nil
	}
	for _, c := range conns {
		cctx, cancel := context.WithTimeout(ctx, runtimeClaimTimeout)
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		err := c.peer.Call(cctx, "session.items", map[string]any{"session_id": id, "limit": 1}, &page)
		cancel()
		if err == nil {
			n.runtimes.claim(c, id)
			return c, nil
		}
	}
	n.runtimes.unclaim(id)
	return nil, nil
}

// hasOwnSession: this machine has the session itself (a live agent in a terminal, a Claude / Codex / TRAEX record).
func (n *Node) hasOwnSession(id string) bool {
	if _, ok := n.agent(id); ok {
		return true
	}
	if n.transcriptPath(id) != "" {
		return true
	}
	for _, p := range []string{n.codexPath(id), n.traexPath(id)} {
		if _, err := os.Stat(p); p != "" && err == nil {
			return true
		}
	}
	return false
}

// errRuntimeAway: a session of a runtime that is not connected (its app is closed, the plugin is off). Nothing was
// sent; the hub may let the app send it again.
func errRuntimeAway(label string) error {
	return &rpc.Error{Message: fmt.Sprintf("%s 没有连上这台电脑的 Jarvis（%s 没在运行，或插件没装好），没有发出去", label, label),
		Code: "runtime_away", Data: json.RawMessage(`{"delivered":"none"}`)}
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
		// Not in its list (an older session it still has): what the node knows without the list.
		return map[string]any{"session_id": p.SessionID, "agent": h.agent()}, nil, true
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
	list := normalizeRuntimeSessions(h, []proto.Session{res.Session})
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
