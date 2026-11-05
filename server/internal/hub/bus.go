package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The bus (docs/protocol.md「总线」): a service — the assistant (a manager) for now — dials GET /api/v1/bus with a
// token of its own (bustokens.go) and keeps the connection up; the hub starts nothing anywhere. Over it runs
// internal/rpc, one JSON line per WebSocket text message, calls going both ways. A connection first says
// manager.hello; after that the service calls the methods registered with handleBus and publishes the events
// registered with handleBusEvent, and the hub calls it with callBus. The hub knows no service's semantics: each
// method is a handler registered by name. A new connection of an identity replaces the one before; only the current
// one may call or publish.

// busCall answers one method a service calls on its connection c; params is never empty (`{}` at least). An
// *rpc.Error it returns reaches the service with its code.
type busCall func(h *Hub, ctx context.Context, c *busConn, params json.RawMessage) (any, error)

// busEvent takes one event a service published. It runs in the connection's read loop, so it must not block.
type busEvent func(h *Hub, c *busConn, params json.RawMessage)

// The registered methods and events (handleBus, handleBusEvent), filled in by the init functions of the files that
// have the handlers, and only read after that.
var (
	busMethods = map[string]busCall{}
	busEvents  = map[string]busEvent{}
)

// handleBus registers what answers method once a connection said its handshake. Call it from an init function, next
// to the handler:
//
//	func init() { handleBus("notify.request", (*Hub).busNotifyRequest) }
func handleBus(method string, fn busCall) {
	if _, dup := busMethods[method]; dup || method == busHello {
		panic("bus: method registered twice: " + method)
	}
	busMethods[method] = fn
}

// handleBusCall registers fn for method with the codec around it: params decoded into P (invalid_params when they
// do not fit), fn's answer encoded as the result. It is how a hub function with its own types is put on the bus, e.g.
// the assistant's boundary:
//
//	handleBusCall("device.call", func(h *Hub, ctx context.Context, p assistant.DeviceCall) (json.RawMessage, error) {
//		return assistantLink{h}.DeviceCall(ctx, p)
//	})
func handleBusCall[P, R any](method string, fn func(h *Hub, ctx context.Context, p P) (R, error)) {
	handleBus(method, func(h *Hub, ctx context.Context, _ *busConn, params json.RawMessage) (any, error) {
		var p P
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpc.Error{Code: bus.CodeInvalidParams, Message: method + ": " + err.Error()}
		}
		return fn(h, ctx, p)
	})
}

// handleBusEvent registers what takes event (a one-way message) once a connection said its handshake; unregistered
// events are dropped.
func handleBusEvent(event string, fn busEvent) {
	if _, dup := busEvents[event]; dup {
		panic("bus: event registered twice: " + event)
	}
	busEvents[event] = fn
}

func init() {
	handleBus("ping", func(*Hub, context.Context, *busConn, json.RawMessage) (any, error) { return map[string]any{}, nil })
}

// busHello is the handshake: the only call a connection may make before it went through.
const busHello = "manager.hello"

// busAssistant is the manager identity the assistant connects as.
const busAssistant = assistantwire.AssistantID

var errBusOffline = errors.New("not connected to the bus")

// errBusDropped is errBusOffline for a call whose request had gone out when the connection dropped (rpc.ErrDropped):
// the other side may have done it.
var errBusDropped = fmt.Errorf("dropped while the call was out: %w", errBusOffline)

// busState is the live connections and the roster (memory only: a service says its handshake again after a hub
// restart).
type busState struct {
	mu       sync.Mutex
	live     map[*busConn]struct{} // every open connection, handshake or not
	current  map[string]*busConn   // the roster: identity id -> the connection whose handshake went through last
	stopping bool
	// Tests shorten these; zero is the protocol's (internal/bus).
	pingEvery, pingTimeout, helloTimeout time.Duration
	// idents keeps the identities instead of h.store (tests without Postgres).
	idents busStore
}

// busConn is one service connection.
type busConn struct {
	ident   db.BusIdentity
	ws      *websocket.Conn
	peer    *rpc.Peer
	helloed chan struct{} // closed once a handshake went through
	once    sync.Once
	closing sync.Once

	// guarded by busState.mu
	presence busPresence
	replaced bool // a newer connection of the identity took over
	ended    bool
}

// busPresence is a connected identity as the roster has it: who, and where its handshake says it runs.
type busPresence struct {
	ID      string    `json:"id"`
	Role    string    `json:"role"`
	Name    string    `json:"name"`
	Host    string    `json:"host,omitempty"`
	OS      string    `json:"os,omitempty"`
	Arch    string    `json:"arch,omitempty"`
	Deploy  string    `json:"deploy,omitempty"`
	Version string    `json:"version,omitempty"`
	Since   time.Time `json:"since"`
}

// close sends the close frame saying why; the read loop then ends the connection.
func (c *busConn) close(code websocket.StatusCode, reason string) {
	c.closing.Do(func() { go func() { _ = c.ws.Close(code, reason) }() })
}

// busRoutes mounts the bus beside the apps' API: its token is not a device's (auth), and a member hub does not pass
// it on to its center (memberForward).
func (h *Hub) busRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+bus.Path, h.serveBus)
}

// busCaller is the identity a bus request's bearer token names (the header only: a token in a URL ends up in logs).
// It answers 403 / 401 / 429 itself, as the gate and auth do for the apps: plaintext from the internet is refused,
// and failures from there count against the source like a device token's.
func (h *Hub) busCaller(w http.ResponseWriter, r *http.Request) (db.BusIdentity, bool) {
	public := h.sourceOf(r) == sourcePublic
	if public && r.TLS == nil && !h.cfg.Security.AllowPublicPlaintext {
		h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
		return db.BusIdentity{}, false
	}
	if public {
		if retry := h.devtok.failures.retry(authSourceKey(r), time.Now(), h.cfg.Security); retry > 0 {
			retryAfter(w, retry)
			h.securityError(w, r, http.StatusTooManyRequests, "rate_limited", 0)
			return db.BusIdentity{}, false
		}
	}
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ident, ok := h.busIdentity(r.Context(), tok); ok && ident.Role == bus.RoleManager { // the only role so far
		return ident, true
	}
	failures := 0
	if public {
		failures = h.devtok.failures.fail(authSourceKey(r), time.Now(), h.cfg.Security)
	}
	h.securityError(w, r, http.StatusUnauthorized, "", failures)
	return db.BusIdentity{}, false
}

func (h *Hub) serveBus(w http.ResponseWriter, r *http.Request) {
	ident, ok := h.busCaller(w, r)
	if !ok {
		return
	}
	if !h.enterWS() {
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	defer h.wsHandlers.Done()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // services are not browsers: no Origin to check
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	// The read loop is bound to the request, not to a hang-up: a connection closed with a reason still sends its
	// close frame.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn := bus.NewConn(ctx, ws)
	c := &busConn{ident: ident, ws: ws, peer: rpc.New("h", conn), helloed: make(chan struct{})}
	c.peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return h.busRequest(ctx, c, method, params)
	}
	c.peer.OnEvent = func(event string, params json.RawMessage) { h.busEvent(c, event, params) }
	if !h.busAdd(c) {
		_ = ws.Close(websocket.StatusGoingAway, "hub stopping")
		return
	}
	defer h.busEnded(c)
	go func() {
		err := bus.Keepalive(ctx, ws, durationOr(h.bus.pingEvery, bus.PingEvery), durationOr(h.bus.pingTimeout, bus.PingTimeout))
		if ctx.Err() == nil {
			h.logf("bus: [%s] %v: hanging up", ident.ID, err)
			_ = ws.CloseNow()
		}
	}()
	go func() {
		limit := durationOr(h.bus.helloTimeout, bus.HelloTimeout)
		t := time.NewTimer(limit)
		defer t.Stop()
		select {
		case <-c.helloed:
		case <-ctx.Done():
		case <-t.C:
			h.logf("bus: [%s] no %s within %s: hanging up", ident.ID, busHello, limit)
			c.close(bus.CloseRefused, "no handshake in time")
		}
	}()
	_ = c.peer.Serve(ctx, conn)
}

func durationOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// busRequest is the connection's dispatcher: its handshake, then busMethods on the current connection only.
func (h *Hub) busRequest(ctx context.Context, c *busConn, method string, params json.RawMessage) (any, error) {
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{}`)
	}
	if method == busHello {
		return h.managerHello(ctx, c, params)
	}
	if !h.busCurrent(c) {
		return nil, &rpc.Error{Code: bus.CodeStaleConnection, Message: "say " + busHello + " on the current connection first"}
	}
	fn, ok := busMethods[method]
	if !ok {
		return nil, &rpc.Error{Code: bus.CodeUnknownMethod, Message: "unknown method " + clip(method, 80)}
	}
	return fn(h, ctx, c, params)
}

func (h *Hub) busEvent(c *busConn, event string, params json.RawMessage) {
	if !h.busCurrent(c) {
		return // before the handshake, or replaced: not heard
	}
	if f := busEvents[event]; f != nil {
		f(h, c, params)
	}
}

// refuse answers a handshake it will not take and hangs up a moment later, once the answer went out.
func (c *busConn) refuse(h *Hub, err error) (any, error) {
	h.logf("bus: [%s] %v: hanging up", c.ident.ID, err)
	time.AfterFunc(200*time.Millisecond, func() { c.close(bus.CloseRefused, "wrong identity") })
	return nil, &rpc.Error{Code: bus.CodeWrongIdentity, Message: err.Error()}
}

// busManagerHello is what manager.hello says that the hub keeps for now; the rest (apps, the machine's summary) is
// the follow-up's.
type busManagerHello struct {
	ID      string `json:"id"`
	Host    string `json:"host"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Deploy  string `json:"deploy"`
	Version string `json:"version"`
}

// busHelloReply is manager.hello's answer: who the hub knows the service as, the managers, and the assistant's hello
// (assistantwire.Hello: the user's profile as the hub has it, where the assistant runs; empty when not set).
type busHelloReply struct {
	Name     string            `json:"name"`
	Role     string            `json:"role"`
	Managers []busManagerBrief `json:"managers"`
	assistantwire.Hello
}

type busManagerBrief struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Online bool   `json:"online"`
}

func (h *Hub) managerHello(ctx context.Context, c *busConn, params json.RawMessage) (any, error) {
	var p busManagerHello
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpc.Error{Code: bus.CodeInvalidParams, Message: err.Error()}
	}
	if p.ID != c.ident.ID {
		return c.refuse(h, fmt.Errorf("manager.hello says %q but the token is %q's", clip(p.ID, 64), c.ident.ID))
	}
	if err := h.busAccept(ctx, c, busPresence{Host: clip(p.Host, 200), OS: clip(p.OS, 40), Arch: clip(p.Arch, 40),
		Deploy: clip(p.Deploy, 40), Version: clip(p.Version, 80)}); err != nil {
		return nil, err
	}
	return busHelloReply{Name: c.ident.Name, Role: c.ident.Role, Managers: h.busManagers(ctx), Hello: h.assistantHello(ctx)}, nil
}

// busAccept makes c its identity's current connection, and hangs up the one it replaces.
func (h *Hub) busAccept(ctx context.Context, c *busConn, p busPresence) error {
	p.ID, p.Role, p.Name, p.Since = c.ident.ID, c.ident.Role, c.ident.Name, time.Now().UTC()
	h.bus.mu.Lock()
	if c.replaced || c.ended {
		h.bus.mu.Unlock()
		return &rpc.Error{Code: bus.CodeStaleConnection, Message: "a newer connection took over"}
	}
	if h.bus.current == nil {
		h.bus.current = map[string]*busConn{}
	}
	old := h.bus.current[c.ident.ID]
	h.bus.current[c.ident.ID] = c
	c.presence = p
	if old != nil && old != c {
		old.replaced = true
	}
	h.bus.mu.Unlock()
	c.once.Do(func() { close(c.helloed) })
	if old == c {
		return nil // a second handshake on the same connection
	}
	if old != nil {
		old.close(bus.CloseReplaced, "replaced by a newer connection")
	}
	h.logf("bus: [%s] connected: host %q %s/%s, version %q", p.ID, p.Host, p.OS, p.Arch, p.Version)
	h.busRosterChanged(ctx, c)
	if c.ident.ID == assistantwire.AssistantID {
		go h.assistantConnected()
	}
	return nil
}

func (h *Hub) busAdd(c *busConn) bool {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	if h.bus.stopping {
		return false
	}
	if h.bus.live == nil {
		h.bus.live = map[*busConn]struct{}{}
	}
	h.bus.live[c] = struct{}{}
	return true
}

func (h *Hub) busEnded(c *busConn) {
	h.bus.mu.Lock()
	delete(h.bus.live, c)
	c.ended = true
	current := h.bus.current[c.ident.ID] == c
	if current {
		delete(h.bus.current, c.ident.ID)
	}
	h.bus.mu.Unlock()
	if current {
		h.logf("bus: [%s] disconnected", c.ident.ID)
		if c.ident.ID == assistantwire.AssistantID {
			h.dropFollows() // the conversations its channels followed (assistant_channels.go)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.busRosterChanged(ctx, nil)
	}
}

func (h *Hub) busCurrent(c *busConn) bool {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	return h.bus.current[c.ident.ID] == c
}

// busConnOf is identity id's current connection, nil while it has none.
func (h *Hub) busConnOf(id string) *busConn {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	return h.bus.current[id]
}

// busRoster is who is connected now, by id.
func (h *Hub) busRoster() []busPresence {
	h.bus.mu.Lock()
	out := make([]busPresence, 0, len(h.bus.current))
	for _, c := range h.bus.current {
		out = append(out, c.presence)
	}
	h.bus.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// busManagers are the managers the hub has tokens for, and which of them are connected.
func (h *Hub) busManagers(ctx context.Context) []busManagerBrief {
	out := []busManagerBrief{}
	s := h.busStore()
	if s == nil {
		return out
	}
	list, err := s.BusIdentities(ctx)
	if err != nil {
		h.logf("bus: identities: %v", err)
	}
	for _, b := range list {
		out = append(out, busManagerBrief{ID: b.ID, Name: b.Name, Role: b.Role, Online: h.busConnOf(b.ID) != nil})
	}
	return out
}

// busRosterChanged tells the connected managers who is online now (event managers), except the one whose
// handshake answer already says so.
func (h *Hub) busRosterChanged(ctx context.Context, except *busConn) {
	list := h.busManagers(ctx)
	h.bus.mu.Lock()
	var to []*busConn
	for _, c := range h.bus.current {
		if c != except {
			to = append(to, c)
		}
	}
	h.bus.mu.Unlock()
	for _, c := range to {
		_ = c.peer.Notify("managers", map[string]any{"managers": list})
	}
}

// callBus calls method on service id's current connection: errBusOffline while it has none, or when it drops
// meanwhile (errBusDropped, which is errBusOffline too, when the request had gone out). Without a deadline on ctx it
// gives up after two minutes.
func (h *Hub) callBus(ctx context.Context, id, method string, params, out any) error {
	c := h.busConnOf(id)
	if c == nil {
		return fmt.Errorf("%s: %w", id, errBusOffline)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	if params == nil {
		params = map[string]any{}
	}
	err := c.peer.Call(ctx, method, params, out)
	switch {
	case errors.Is(err, rpc.ErrDropped):
		return fmt.Errorf("%s: %w", id, errBusDropped)
	case errors.Is(err, rpc.ErrClosed):
		return fmt.Errorf("%s: %w", id, errBusOffline)
	}
	return err
}

// notifyBus sends service id a one-way event on its current connection.
func (h *Hub) notifyBus(id, event string, params any) error {
	c := h.busConnOf(id)
	if c == nil {
		return fmt.Errorf("%s: %w", id, errBusOffline)
	}
	return c.peer.Notify(event, params)
}

// busHangUp closes every open connection of identity id, with code and reason.
func (h *Hub) busHangUp(id string, code websocket.StatusCode, reason string) {
	h.bus.mu.Lock()
	var cut []*busConn
	for c := range h.bus.live {
		if c.ident.ID == id {
			cut = append(cut, c)
		}
	}
	h.bus.mu.Unlock()
	for _, c := range cut {
		c.close(code, reason)
	}
}

// stopBus refuses new connections and returns the open ones, for the shutdown to close (stopWebSockets).
func (h *Hub) stopBus() []*busConn {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	h.bus.stopping = true
	out := make([]*busConn, 0, len(h.bus.live))
	for c := range h.bus.live {
		out = append(out, c)
	}
	return out
}
