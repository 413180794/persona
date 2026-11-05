package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The assistant (internal/assistant) is a process of its own on the bus (docs/assistant.md「和 hub 的边界」); the hub
// starts it beside itself (assistant_process.go). This file is the hub's end: assistantLink answers the assistant's
// calls (manager.hello, device.call, app.call, notify.request, manager.snapshot, ws.broadcast; registered on the bus
// below) with the hub's own functions, the hub keeps the snapshot the assistant last reported (the session lists the
// apps get are decorated with it), and assistantRemote is what the hub asks of the assistant, as messages on its
// connection. The hub never runs the assistant's code.

// assistantState is what the assistant last reported and told the hub.
type assistantState struct {
	mu       sync.RWMutex
	got      bool // a snapshot came in
	partial  bool
	epoch    string
	seq      uint64
	sessions map[string]assistantwire.SessionView // by session id; replaced, never written into
	tasks    []assistantwire.TaskView             // nil while its schedule could not be read; replaced, never written into
	profile  *assistantwire.Profile               // its settings (assistant_profile.go); nil before it reported them

	resyncAt atomic.Int64 // when the hub last asked for the whole snapshot (UnixNano)

	toolsMu sync.Mutex
	tools   []proto.Tool // its MCP tools (assistant.tools), asked for when it connects

	helloMu   sync.Mutex
	lastHello string      // the hello answer it last got (manager.hello, manager.updated), as JSON
	updating  atomic.Bool // a manager.updated check is on its way

	askMu sync.Mutex
	asked map[string]*askedOne // the questions and cards it has on the phone (by interaction id)
}

// taskSettleDelay is how long the assistant lets a task's agents stay still before the task counts as done: the
// delay of the hub's own finished push (tests shorten it).
var taskSettleDelay = func(c NotifyConfig) time.Duration { return c.doneDelay() }

// assistantRoutesRelayed relays the apps' requests on the assistant's routes to it (http.request, forwardHTTP: the
// caller's role goes with them); while it is not connected they are answered 503 assistant_offline.
func (h *Hub) assistantRoutesRelayed(api *http.ServeMux) {
	for _, pattern := range assistantwire.Routes {
		api.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { h.forwardHTTP(w, r, assistantwire.AssistantID) })
	}
}

// assistantShown is what the assistant reported about its conversations, by session id.
func (h *Hub) assistantShown() map[string]assistantwire.SessionView {
	a := h.assistantReported()
	defer a.mu.RUnlock()
	return a.sessions
}

// assistantTasks are the assistant's scheduled tasks as it reported them; false while it could not read them.
func (h *Hub) assistantTasks() ([]assistantwire.TaskView, bool) {
	a := h.assistantReported()
	defer a.mu.RUnlock()
	return a.tasks, a.tasks != nil
}

// quietTaskConversation: session is the own conversation of a scheduled task that pushes only what its runs send
// (TaskView.Push, #2396), as the assistant last reported its tasks. By the session alone: an assistant conversation's
// id is unique, and a machine id the assistant had from an older alias would miss it.
func (h *Hub) quietTaskConversation(session string) bool {
	tasks, _ := h.assistantTasks()
	for _, t := range tasks {
		if t.Push == assistantwire.PushAsked && t.RunSessionID != "" && t.RunSessionID == session {
			return true
		}
	}
	return false
}

// quietTaskFinish: s's finished turn is not pushed: a quiet task's own conversation (quietTaskConversation) that may
// go unpushed now (assistantwire.QuietTaskFinish: Claude Code or a headless Codex, not the main conversation, a normal
// finish).
func (h *Hub) quietTaskFinish(s proto.Session) bool {
	a := &h.assistant
	a.mu.RLock()
	p := a.profile
	a.mu.RUnlock()
	if p == nil {
		return false // the assistant's settings (its main conversation) not known yet: pushed as ever
	}
	return h.quietTaskConversation(s.SessionID) && assistantwire.QuietTaskFinish(s, p.Main)
}

// assistantReported is the last snapshot, read-locked. With none yet, or a partial one (the database did not answer
// it), the assistant is asked for the whole of it again (manager.resync, at most every few seconds); it answers with
// a snapshot, which pushes the lists it changes.
func (h *Hub) assistantReported() *assistantState {
	a := &h.assistant
	a.mu.RLock()
	need := !a.got || a.partial
	a.mu.RUnlock()
	if now := time.Now().UnixNano(); need && now-a.resyncAt.Load() > int64(5*time.Second) {
		a.resyncAt.Store(now)
		_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventResync, map[string]any{})
	}
	a.mu.RLock()
	return a
}

// assistantLink is the hub's side of assistantwire.Hub.
type assistantLink struct{ h *Hub }

// Hello is hub.json's assistant section (the defaults of the assistant's own settings), the machines, and what of
// the hub's own the assistant needs.
func (l assistantLink) Hello(context.Context) (assistantwire.Hello, error) {
	h := l.h
	out := assistantwire.Hello{KB: h.kbSettings(), Locale: string(h.userLang())} // hub.json's kb section: the knowledge base it keeps (kb.go)
	if h.cfg == nil {
		return out, nil
	}
	out.Owner = strings.TrimSpace(h.cfg.Assistant.Owner)
	out.OwnerNotes = strings.TrimSpace(h.cfg.Assistant.Notes)
	out.Assistant = assistantwire.Placement{MachineID: h.defaultAssistantMachine(), Launcher: h.cfg.Assistant.Launcher, Standby: h.isMember()}
	out.Assistant.Machines = h.machineIDs()
	out.DoneDelayMS = taskSettleDelay(h.cfg.Notify).Milliseconds()
	// The admin UI may have switched it since the hub started (admin_assistant.go).
	out.PersonaView = h.assistantPersonaView()
	out.Agents = h.cfg.Agents  // the Agents business's defaults (agents_link.go)
	out.Models = h.hubModels() // what hub.json says about models (assistant_models.go)
	return out, nil
}

func (l assistantLink) DeviceCall(ctx context.Context, p assistantwire.DeviceCall) (json.RawMessage, error) {
	h := l.h
	if p.Device == assistantwire.DeviceAll {
		switch p.Method {
		case assistantwire.DeviceDevicesList:
			list := []assistantwire.Device{}
			for _, m := range h.machinesNow() {
				m.mu.Lock()
				online, info := m.online, m.info
				m.mu.Unlock()
				list = append(list, assistantwire.Device{ID: m.cfg.ID, Name: m.cfg.Name, Kind: m.cfg.Kind, SSHHost: m.cfg.SSHHost, Home: info.Home,
					DataDir: info.DataDir, Online: online, Reachable: m.reachable(), Version: info.Version,
					ModelConfiguration: info.ModelConfiguration})
			}
			return json.Marshal(map[string]any{"devices": list, "phone_online": h.phoneClient() != nil})
		case assistantwire.DeviceInteractionsList:
			return json.Marshal(map[string]any{"interactions": h.pendingInteractions()})
		}
	}
	m := h.localMachine(p.Device)
	if m == nil {
		return nil, noDevice(p.Device)
	}
	if out, ok, err := h.channelsDeviceCall(ctx, m, p); ok { // following a turn, sending once (assistant_channels.go)
		return out, err
	}
	switch p.Method {
	case assistantwire.DeviceSessionsList:
		list := h.decorated(m)
		return json.Marshal(map[string]any{"sessions": list, "muted": h.mutedSessions(m.cfg.ID, list)})
	case assistantwire.DeviceSessionGet:
		var a struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, err
		}
		s, ok := m.session(a.SessionID)
		if ok {
			s.Assistant = isAssistant(s, m.assistantCwd())
			if s.Assistant {
				s.Space = assistantSpace(s.Cwd)
			}
		}
		return json.Marshal(map[string]any{"session": s, "ok": ok})
	}
	var out json.RawMessage
	if err := m.call(ctx, p.Method, p.Params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (l assistantLink) AppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, error) {
	h := l.h
	if out, ok, err := h.assistantAppCall(ctx, p); ok { // assistant_apps.go
		return out, err
	}
	if out, ok, err := h.modelsAppCall(ctx, p); ok { // the plugins' providers and models (assistant_models.go)
		return out, err
	}
	if out, ok, err := h.channelsAppCall(ctx, p); ok { // the channels' transport (assistant_channels.go)
		return out, err
	}
	if out, ok, err := h.kbSourcesCall(ctx, p); ok { // the knowledge base's sources (kb_sources.go)
		return out, err
	}
	if out, ok, err := h.inboxAppCall(ctx, p); ok { // the inbox's sources (inbox.go)
		return out, err
	}
	switch p.AppID + " " + p.Method {
	case assistantwire.AppConnectors + " " + assistantwire.ConnectorsSnapshots:
		var a struct {
			TZ string `json:"tz"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"snapshots": h.connectorSnapshots(ctx, a.TZ)})
	case assistantwire.AppInteractions + " " + assistantwire.InteractionsGet,
		assistantwire.AppInteractions + " " + assistantwire.InteractionsAnswer:
		out, err := h.interactionsCall(ctx, p.Method, p.Params)
		if err != nil {
			return nil, err
		}
		return json.Marshal(out)
	}
	if p.Method == assistantwire.AppTool {
		return h.appTool(ctx, p.AppID, p.Params)
	}
	return nil, fmt.Errorf("app %s has no method %s", p.AppID, p.Method)
}

// appTool runs the one of the assistant's tools that is still the hub's to run: the hub's own settings
// (jarvis_settings without the assistant's, settings_tool.go there).
func (h *Hub) appTool(ctx context.Context, app string, params json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		MachineID string          `json:"machine_id"`
		SessionID string          `json:"session_id"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, err
	}
	if len(a.Arguments) == 0 {
		a.Arguments = json.RawMessage(`{}`)
	}
	var res string
	var err error
	switch app + " " + a.Name {
	case assistantwire.AppSettings + " jarvis_settings":
		m := h.localMachine(a.MachineID)
		if m == nil {
			return nil, fmt.Errorf("%w %q", assistantwire.ErrNoDevice, a.MachineID)
		}
		res, err = h.toolJarvisSettings(ctx, m, a.SessionID, a.Arguments)
	default:
		return nil, fmt.Errorf("app %s has no tool %s", app, a.Name)
	}
	if err != nil {
		return json.Marshal(proto.TextResult(err.Error(), true))
	}
	return json.Marshal(proto.TextResult(res, false))
}

// NotifyRequest pushes what the assistant asks, like any agent's push (every device whose settings take it).
func (l assistantLink) NotifyRequest(_ context.Context, p assistantwire.NotifyRequest) error {
	h := l.h
	n := p.Notification
	if n.Kind != assistantwire.NotifyPush {
		return fmt.Errorf("unknown notification kind %q", n.Kind)
	}
	h.notifyDone("assistant", n.DeviceID, n.Subtitle, n.SessionID, n.Title, n.Body, pushWants{})
	if n.Asked { // the conversation pushed what matters itself (notify_user; the assistant sends it to the channels)
		h.mu.Lock()
		if h.notified == nil {
			h.notified = map[string]time.Time{}
		}
		h.notified[n.SessionID] = time.Now()
		h.mu.Unlock()
	}
	return nil
}

// Broadcast sends the assistant's live update to the apps as it is (ws.broadcast, relayBroadcast).
func (l assistantLink) Broadcast(_ context.Context, b assistantwire.Broadcast) {
	if err := l.h.relayBroadcast(b.Message); err != nil {
		l.h.logf("assistant: broadcast: %v", err)
	}
}

// Snapshot keeps what the assistant reported and pushes the session lists of the machines whose conversations look
// different now.
func (l assistantLink) Snapshot(_ context.Context, s assistantwire.Snapshot) (assistantwire.SnapshotResult, error) {
	h := l.h
	a := &h.assistant
	views := make(map[string]assistantwire.SessionView, len(s.Sessions))
	for _, v := range s.Sessions {
		views[v.SessionID] = v
	}
	a.mu.Lock()
	if a.got && s.Epoch == a.epoch && s.Seq <= a.seq {
		a.mu.Unlock()
		return assistantwire.SnapshotResult{}, assistantwire.ErrStaleSnapshot
	}
	old, oldProfile := a.sessions, a.profile
	a.got, a.partial, a.epoch, a.seq, a.sessions, a.tasks = true, s.Partial, s.Epoch, s.Seq, views, s.Tasks
	if s.Profile != nil {
		a.profile = s.Profile
	}
	a.mu.Unlock()
	if s.Profile != nil && !jsonSame(oldProfile, s.Profile) {
		h.keepAssistantProfile(context.Background(), s.Profile)
	}
	// What the nodes keep listing (s.Profile.Keep) the manager tells them itself (internal/devices SetKeep).
	h.takeInboxReport(s.Inbox) // inbox.go
	h.askOnPhone(s.Interactions)
	machines := changedMachines(old, views)
	for _, mid := range h.takeTodoReport(s.Todos) { // the todos' (assistant_todos.go)
		if !slices.Contains(machines, mid) {
			machines = append(machines, mid)
		}
	}
	for _, mid := range h.takeWorkReport(s.Work) { // the agents' work (agents_link.go)
		if !slices.Contains(machines, mid) {
			machines = append(machines, mid)
		}
	}
	for _, mid := range h.takeChannelsReport(s.Channels) { // the channels' (assistant_channels.go)
		if !slices.Contains(machines, mid) {
			machines = append(machines, mid)
		}
	}
	for _, mid := range machines {
		if m := h.localMachine(mid); m != nil {
			h.sessionsChanged(m)
		}
	}
	go h.deliverAnswers() // answers kept for it: it is there, with its state read back (agents_link.go)
	return assistantwire.SnapshotResult{OK: true, Seq: s.Seq}, nil
}

// changedMachines are the machines of the conversations whose views differ between two snapshots.
func changedMachines(old, now map[string]assistantwire.SessionView) []string {
	var out []string
	add := func(mid string) {
		if !slices.Contains(out, mid) {
			out = append(out, mid)
		}
	}
	for id, v := range now {
		o, ok := old[id]
		if !ok || o.Title != v.Title || o.Archived != v.Archived || !slices.Equal(o.Queued, v.Queued) || o.MachineID != v.MachineID {
			add(v.MachineID)
			if ok {
				add(o.MachineID)
			}
		}
	}
	for id, o := range old {
		if _, ok := now[id]; !ok {
			add(o.MachineID)
		}
	}
	return out
}

// askOnPhone shows the questions the assistant asks and its cards about agents' turns (its snapshot's interactions)
// like any agent's card: recorded, announced, pushed, expired by the hub (waitInteraction); the answer goes back to
// the assistant (manager.action), or waits for it when it cannot take it (keepAnswer). A question that left the
// snapshot is withdrawn, as a hook's that went away, and nothing is said about it. One it still reports a while after
// its answer went, with no answer waiting for it, is shown again: the answer did not take.
func (h *Hub) askOnPhone(list []proto.Interaction) {
	a := &h.assistant
	a.askMu.Lock()
	defer a.askMu.Unlock()
	if a.asked == nil {
		a.asked = map[string]*askedOne{}
	}
	now := map[string]bool{}
	for _, ix := range list {
		now[ix.ID] = true
		if q := a.asked[ix.ID]; q != nil {
			if q.ended.IsZero() || time.Since(q.ended) < askAgainAfter || h.answerKept(ix.ID) {
				continue
			}
			q.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		q := &askedOne{cancel: cancel}
		a.asked[ix.ID] = q
		go func(ix proto.Interaction) {
			r, err := h.waitInteraction(ctx, ix)
			if err != nil || r.Dismiss && ctx.Err() != nil {
				return // withdrawn: it knows
			}
			answer, _ := json.Marshal(r)
			act := assistantwire.Action{ActionID: "act_" + randomHex(6), SessionID: ix.SessionID, Op: "answer", InteractionID: ix.ID,
				InteractionVersion: 1, Answer: answer}
			// Not taken (not connected, not ready): it gets the answer later (agents_link.go).
			if _, err := h.asst().Action(context.Background(), act); err != nil && !h.keepAnswer(act, err, ix.ExpiresAt) {
				h.logf("assistant: answer %s: %v", ix.ID, err)
			}
			a.askMu.Lock()
			q.ended = time.Now()
			a.askMu.Unlock()
		}(ix)
	}
	for id, q := range a.asked {
		if !now[id] {
			q.cancel()
			delete(a.asked, id)
		}
	}
}

// askedOne is a question or card of the assistant's on the phone: cancel withdraws it; ended is when its answer (or
// its expiry) went to the assistant.
type askedOne struct {
	cancel context.CancelFunc
	ended  time.Time
}

// askAgainAfter is how long a question the assistant still reports after its answer went may stay off the phone.
var askAgainAfter = 30 * time.Second

// ---- on the bus: what the assistant calls ----

func init() {
	handleBusCall(assistantwire.MethodDevice, func(h *Hub, ctx context.Context, p assistantwire.DeviceCall) (json.RawMessage, error) {
		out, err := assistantLink{h}.DeviceCall(ctx, p)
		return out, busCodedError(err)
	})
	handleBusCall(assistantwire.MethodApp, func(h *Hub, ctx context.Context, p assistantwire.AppCall) (json.RawMessage, error) {
		if p.TimeoutMS > 0 { // the assistant stops waiting then: so does the hub (a model call)
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
			defer cancel()
		}
		out, err := assistantLink{h}.AppCall(ctx, p)
		return out, busCodedError(err)
	})
	handleBusCall(assistantwire.MethodNotify, func(h *Hub, ctx context.Context, p assistantwire.NotifyRequest) (map[string]bool, error) {
		if err := (assistantLink{h}).NotifyRequest(ctx, p); err != nil {
			return nil, busCodedError(err)
		}
		return map[string]bool{"ok": true}, nil
	})
	handleBusCall(assistantwire.MethodSnapshot, func(h *Hub, ctx context.Context, s assistantwire.Snapshot) (assistantwire.SnapshotResult, error) {
		res, err := assistantLink{h}.Snapshot(ctx, s)
		return res, busCodedError(err)
	})
	// ws.broadcast is relayBroadcast's (bus_forward.go), the same as Broadcast's.
}

// busCodedError puts what the assistant's code tells errors apart by on the wire as a code: a machine the hub does
// not have, one that is offline, a node link that dropped, a stale snapshot, a paused model. A node's own coded
// refusal (*rpc.Error) goes as it is; anything else as its message.
func busCodedError(err error) error {
	var re *rpc.Error
	switch {
	case err == nil || errors.As(err, &re):
		return err
	case errors.Is(err, assistantwire.ErrNoDevice):
		return &rpc.Error{Code: assistantwire.CodeNoDevice, Message: err.Error()}
	case errors.Is(err, errOffline):
		return &rpc.Error{Code: assistantwire.CodeMachineOffline, Message: err.Error()}
	case errors.Is(err, rpc.ErrClosed):
		return &rpc.Error{Code: assistantwire.CodeNodeClosed, Message: err.Error()}
	case errors.Is(err, assistantwire.ErrStaleSnapshot):
		return &rpc.Error{Code: assistantwire.CodeStaleSnapshot, Message: err.Error()}
	}
	return err
}

// assistantHello is manager.hello's answer for the assistant, kept as what it last got.
func (h *Hub) assistantHello(ctx context.Context) assistantwire.Hello {
	hello, _ := assistantLink{h}.Hello(ctx)
	raw, _ := json.Marshal(hello)
	a := &h.assistant
	a.helloMu.Lock()
	a.lastHello = string(raw)
	a.helloMu.Unlock()
	return hello
}

// assistantUpdated tells the connected assistant its hello answer changed (manager.updated): this hub's mesh role, the
// machines (and with them the default machine). It is called wherever one of them may have changed, often (every
// machine update): bursts are taken together, and only a different answer is sent.
func (h *Hub) assistantUpdated() {
	a := &h.assistant
	if !a.updating.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		a.updating.Store(false)
		if h.busConnOf(assistantwire.AssistantID) == nil {
			return // it gets a fresh one when it connects
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hello, _ := assistantLink{h}.Hello(ctx)
		raw, _ := json.Marshal(hello)
		a.helloMu.Lock()
		same := a.lastHello == string(raw)
		a.lastHello = string(raw)
		a.helloMu.Unlock()
		if !same {
			_ = h.notifyBus(assistantwire.AssistantID, assistantwire.EventUpdated, hello)
		}
	}()
}

// assistantConnected runs when the assistant's handshake went through: its tools are asked for again (a new build
// may have others) and kept for when it is not connected (its tools then answer assistant_offline); what waited for it
// goes.
func (h *Hub) assistantConnected() {
	go h.inboxManagerConnected() // its inbox gets a full round (inbox.go)
	h.kbFlush()                  // the agents' turns that waited for it (kb.go)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res struct {
		Tools []proto.Tool `json:"tools"`
	}
	h.managerConnected() // agents_link.go
	generation := h.assistantToolsGeneration()
	if err := h.callBus(ctx, assistantwire.AssistantID, assistantwire.MethodTools, nil, &res); err != nil {
		h.logf("assistant: tools: %v", err)
		return
	}
	if !h.storeAssistantTools(generation, res.Tools) {
		return
	}
	// Connectors register as the hub starts, before the assistant says its tools (builtinTools): one that took a name
	// of the assistant's gets its calls (connectors are asked first), which is said here.
	for _, t := range res.Tools {
		if h.connectors != nil && h.connectors.Offers(t.Name) {
			h.logf("assistant: a connector offers %s, the assistant's tool: the connector answers it", t.Name)
		}
	}
}

// ---- on the bus: what the hub asks of the assistant ----

// asst is the assistant, reached over the bus.
func (h *Hub) asst() assistantRemote { return assistantRemote{h} }

// assistantRemote is assistant.Handler over the assistant's bus connection: calls for what the hub needs an answer
// to, one-way events for what it saw happen. Not connected, a call fails with assistant_offline and an event is not
// sent (the assistant asks for what it missed when it is back: its snapshot, the session lists).
type assistantRemote struct{ h *Hub }

// errAssistantOffline is what an assistant tool or request answers while the assistant is not connected.
var errAssistantOffline = i18n.Err("hub-assistant_link.offline")

func (a assistantRemote) call(ctx context.Context, method string, params, out any) error {
	err := a.h.callBus(ctx, assistantwire.AssistantID, method, params, out)
	if errors.Is(err, errBusOffline) {
		return &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: errAssistantOffline.Error()}
	}
	return err
}

func (a assistantRemote) notify(event string, params any) {
	_ = a.h.notifyBus(assistantwire.AssistantID, event, params)
}

// assistantToolsWait is how long a question about the assistant's tools waits for it while it has not connected since
// the hub started (tests shorten it).
var assistantToolsWait = 10 * time.Second

// Tools are the assistant's own MCP tools, as it said when it last connected; while it has not connected since the
// hub started, the hub waits a little for it (a node that asks right then keeps the list for its conversation's life).
func (a assistantRemote) Tools(ctx context.Context) []proto.Tool {
	for wait := time.Now().Add(assistantToolsWait); ; {
		tools := a.h.assistantToolsNow()
		if tools != nil || time.Now().After(wait) || ctx.Err() != nil {
			return tools
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// assistantToolsNow are the assistant's tools as it last listed them; nil before it first connected.
func (h *Hub) assistantToolsNow() []proto.Tool {
	st := &h.assistant
	st.toolsMu.Lock()
	defer st.toolsMu.Unlock()
	return st.tools
}

// Tool runs one of the assistant's tools (assistant.tool). ask_user and jarvis_settings wait for the phone: up to
// the question's half hour, and then some.
func (a assistantRemote) Tool(ctx context.Context, call assistantwire.ToolCall) proto.ToolResult {
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	var res proto.ToolResult
	if err := a.call(ctx, assistantwire.MethodTool, call, &res); err != nil {
		return proto.TextResult(err.Error(), true)
	}
	return res
}

func (a assistantRemote) Context(ctx context.Context, p assistantwire.ContextParams) (assistantwire.ContextResult, error) {
	var res assistantwire.ContextResult
	err := a.call(ctx, assistantwire.MethodContext, p, &res)
	return res, err
}

func (a assistantRemote) Action(ctx context.Context, act assistantwire.Action) (assistantwire.ActionResult, error) {
	var res assistantwire.ActionResult
	err := a.call(ctx, assistantwire.MethodAction, act, &res)
	return res, err
}

func (a assistantRemote) UserSpoke(_ context.Context, r assistantwire.SessionRef) {
	a.notify(assistantwire.EventUserSpoke, r)
}

func (a assistantRemote) TurnStarted(_ context.Context, r assistantwire.SessionRef) {
	a.notify(assistantwire.EventTurnStarted, r)
}

func (a assistantRemote) TurnEnded(_ context.Context, r assistantwire.SessionRef) {
	a.notify(assistantwire.EventTurnEnded, r)
}

func (a assistantRemote) SessionsChanged(_ context.Context, d assistantwire.DeviceSessions) {
	a.notify(assistantwire.EventSessionsChanged, d)
}

func (a assistantRemote) InteractionResolved(_ context.Context, r assistantwire.InteractionResolved) {
	a.notify(assistantwire.EventInteractionResolved, r)
}

func (a assistantRemote) InteractionRaised(_ context.Context, r assistantwire.InteractionRaised) {
	a.notify(assistantwire.EventInteractionRaised, r)
}

// The todos' (assistant_todos.go).

func (a assistantRemote) TodoHandoff(ctx context.Context, p assistantwire.TodoHandoff) (assistantwire.TodoHandoffResult, error) {
	var res assistantwire.TodoHandoffResult
	err := a.call(ctx, assistantwire.MethodTodoHandoff, p, &res)
	return res, err
}

func (a assistantRemote) TodoLink(ctx context.Context, p assistantwire.TodoLinkParams) error {
	return a.call(ctx, assistantwire.MethodTodoLink, p, nil)
}

func (a assistantRemote) TodoSpaceRenamed(_ context.Context, r assistantwire.TodoSpaceRenamed) {
	a.notify(assistantwire.EventTodoSpaceRenamed, r)
}

// ---- the hub's own parts that tell the assistant, or ask it ----

// isAssistantTool: name is one of the assistant's own tools (it lists them: assistant.tools).
func (h *Hub) isAssistantTool(ctx context.Context, name string) bool {
	for _, t := range h.asst().Tools(ctx) {
		if t.Name == name {
			return true
		}
	}
	return false
}

// callAssistantTool runs one of the assistant's tools for the conversation sessionID on machine m (assistant.tool).
func (h *Hub) callAssistantTool(ctx context.Context, m *Machine, sessionID, name string, args json.RawMessage) proto.ToolResult {
	return h.asst().Tool(ctx, assistantwire.ToolCall{Name: name, Arguments: args, MachineID: m.cfg.ID, SessionID: sessionID})
}

// taskAnswered: the user dealt with an agent's card; when it is a task's, its conversation keeps a record of it
// (interaction.resolved).
func (h *Hub) taskAnswered(ix proto.Interaction, r proto.Resolution, status string) {
	h.asst().InteractionResolved(context.Background(), assistantwire.InteractionResolved{Interaction: ix, Resolution: r, Status: status})
}

// quietInteraction: an agent of a quiet task asks something; the assistant tells the main conversation
// (interaction.raised, #1731).
func (h *Hub) quietInteraction(ix proto.Interaction) {
	h.asst().InteractionRaised(context.Background(), assistantwire.InteractionRaised{Interaction: h.withTask(ix)})
}

// ---- what the hub answers the assistant from its other parts ----

// connectorSnapshots are the live states of the signed-in connectors that keep one (connector.Snapshot: a chat service
// through a plugin), asked at once and waited for 3 seconds; those that do not answer in time are left out
// (connectors.snapshots; tz is the user's zone, "" the hub's guess).
func (h *Hub) connectorSnapshots(ctx context.Context, tz string) []connector.SnapshotData {
	out := []connector.SnapshotData{}
	if h.connectors == nil {
		return out
	}
	if tz == "" {
		tz = h.userLocation().String()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range h.connectors.All() {
		sn, ok := connector.As[connector.Snapshot](c)
		if !ok || connector.StatusOf(ctx, c).State != connector.StateOK {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := sn.Snapshot(ctx, tz)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, d)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
