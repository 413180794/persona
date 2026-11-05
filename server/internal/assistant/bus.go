package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The assistant on the hub's bus (docs/protocol.md「总线」): BusHub is Hub over a bus.Client connection, and serves
// the hub's calls and events with a Service. The connection stays up while Run runs: it dials with the assistant's
// token, says manager.hello, keeps the answer (manager.updated replaces it), reports the whole snapshot, and dials
// again when it drops.

// BusOptions are where the hub is and how to reach it.
type BusOptions struct {
	Hub        string // the hub's base URL (http(s)://host:port)
	Token      string // the assistant's bus token (jb_…)
	HTTPClient *http.Client
	Version    string        // this build's, for the hub's roster
	Deploy     string        // how it was started: "child" (the hub's), "external"
	Backoff    time.Duration // before dialing again, doubling to a minute (zero: the protocol's 1 s; tests shorten it)
	Logf       func(format string, args ...any)
}

// BusHub is Hub over the bus.
type BusHub struct {
	client *bus.Client
	opts   BusOptions

	mu    sync.Mutex
	hello Hello
	svc   *Service
	ctx   context.Context // Serve's

	events  chan busEvent // the hub's events, taken in the order they came
	pending atomic.Int64  // events received and not yet taken
}

type busEvent struct {
	name   string
	params json.RawMessage
}

var _ Hub = (*BusHub)(nil)

// NewBusHub is the hub as the assistant reaches it over the bus; Run connects it.
func NewBusHub(o BusOptions) *BusHub {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	b := &BusHub{opts: o, events: make(chan busEvent, 1024)}
	host, _ := os.Hostname()
	b.client = &bus.Client{
		Hub: o.Hub, Token: o.Token, HTTPClient: o.HTTPClient, Logf: o.Logf, Backoff: o.Backoff,
		HelloMethod: assistantwire.MethodHello,
		HelloParams: func() any {
			return map[string]any{"id": assistantwire.AssistantID, "host": host, "os": runtime.GOOS, "arch": runtime.GOARCH,
				"deploy": o.Deploy, "version": o.Version}
		},
		OnHello:   b.onHello,
		OnRequest: b.answer,
		OnEvent: func(event string, params json.RawMessage) {
			b.pending.Add(1)
			select {
			case b.events <- busEvent{event, params}:
			default:
				b.pending.Add(-1)
				o.Logf("assistant: %s dropped: too many events waiting", event)
			}
		},
	}
	return b
}

// Run keeps the connection to the hub up and serves it with svc, and runs svc's own work (its schedule) from when it
// first connected (it needs the hub's hello: where it runs, the user's time zone), until ctx ends.
func (b *BusHub) Run(ctx context.Context, svc *Service) error {
	go func() {
		if b.client.Wait(ctx) == nil {
			svc.Run(ctx)
		}
	}()
	return b.Serve(ctx, svc)
}

// Serve is Run without svc's own work: the connection alone. It lets svc's knowledge base go when it ends.
func (b *BusHub) Serve(ctx context.Context, svc *Service) error {
	b.mu.Lock()
	b.svc, b.ctx = svc, ctx
	b.mu.Unlock()
	defer svc.kbClose()
	go b.takeEvents(ctx)
	return b.client.Run(ctx)
}

// Connected: the connection to the hub is up now (the knowledge base closes while it is not, kb.go).
func (b *BusHub) Connected() bool { return b.client.Peer() != nil }

// onHello keeps the hub's answer to manager.hello. A new connection: the hub shows nothing of the assistant until it
// reports the whole of it again.
func (b *BusHub) onHello(res json.RawMessage) error {
	var h Hello
	if err := json.Unmarshal(res, &h); err != nil {
		return err
	}
	b.setHello(h)
	b.mu.Lock()
	svc, ctx := b.svc, b.ctx
	b.mu.Unlock()
	if svc != nil {
		go svc.Resync(context.WithoutCancel(ctx))
	}
	return nil
}

// Wait returns once the connection is up and its handshake went through.
func (b *BusHub) Wait(ctx context.Context) error { return b.client.Wait(ctx) }

func (b *BusHub) setHello(h Hello) {
	b.mu.Lock()
	b.hello = h
	b.mu.Unlock()
}

// ---- what the assistant asks of the hub ----

func (b *BusHub) Hello(context.Context) (Hello, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hello, nil
}

func (b *BusHub) DeviceCall(ctx context.Context, p DeviceCall) (json.RawMessage, error) {
	var out json.RawMessage
	err := b.client.Call(ctx, assistantwire.MethodDevice, p, &out)
	return out, busError(err)
}

func (b *BusHub) AppCall(ctx context.Context, p AppCall) (json.RawMessage, error) {
	if d, ok := ctx.Deadline(); ok { // the hub stops when the caller stops waiting (a model call it would go on with)
		p.TimeoutMS = max(time.Until(d).Milliseconds(), 1)
	}
	var out json.RawMessage
	err := b.client.Call(ctx, assistantwire.MethodApp, p, &out)
	return out, busError(err)
}

func (b *BusHub) NotifyRequest(ctx context.Context, p NotifyRequest) error {
	return busError(b.client.Call(ctx, assistantwire.MethodNotify, p, nil))
}

func (b *BusHub) Broadcast(_ context.Context, m Broadcast) {
	if err := b.client.Notify(assistantwire.EventBroadcast, m); err != nil {
		b.opts.Logf("assistant: %s: %v", assistantwire.EventBroadcast, err)
	}
}

func (b *BusHub) Snapshot(ctx context.Context, s Snapshot) (SnapshotResult, error) {
	var out SnapshotResult
	err := b.client.Call(ctx, assistantwire.MethodSnapshot, s, &out)
	return out, busError(err)
}

// codedError is an error that came over the bus with a code: its message as the hub wrote it, its kind as the
// assistant's code checks it (errors.Is).
type codedError struct {
	msg  string
	code string // the hub's (assistantwire.Code…)
	kind error
}

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Unwrap() error { return e.kind }

// busError gives the hub's coded errors back their kind: a machine the hub does not have, one that is offline, a
// node link that dropped, a stale snapshot, a model that is paused. Other errors (a node's own coded refusal among
// them) stay *rpc.Error, as they were on the hub.
func busError(err error) error {
	var re *rpc.Error
	if !errors.As(err, &re) {
		return err
	}
	var kind error
	switch re.Code {
	case assistantwire.CodeNoDevice:
		kind = ErrNoDevice
	case assistantwire.CodeMachineOffline:
		kind = apierr.ErrOffline
	case assistantwire.CodeNodeClosed:
		kind = rpc.ErrClosed
	case assistantwire.CodeStaleSnapshot:
		kind = ErrStaleSnapshot
	case assistantwire.CodeModelPaused:
		kind = &ai.PausedError{}
	default:
		return err
	}
	return &codedError{msg: re.Message, code: re.Code, kind: kind}
}

// answer is request with the hub's coded errors given back their code: an answer the assistant could not give because
// of the hub (a machine that is offline, a node link that dropped) reaches the app as the hub's own error would.
func (b *BusHub) answer(ctx context.Context, method string, params json.RawMessage) (any, error) {
	out, err := b.request(ctx, method, params)
	var ce *codedError
	if errors.As(err, &ce) {
		return nil, &rpc.Error{Code: ce.code, Message: ce.msg}
	}
	return out, err
}

// ---- what the hub asks of the assistant ----

func (b *BusHub) service() *Service {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.svc
}

func (b *BusHub) request(ctx context.Context, method string, params json.RawMessage) (any, error) {
	s := b.service()
	decode := func(v any) error {
		if err := json.Unmarshal(params, v); err != nil {
			return &rpc.Error{Code: bus.CodeInvalidParams, Message: method + ": " + err.Error()}
		}
		return nil
	}
	switch method {
	case assistantwire.MethodHTTP:
		var p assistantwire.HTTPRequest
		if err := decode(&p); err != nil {
			return nil, err
		}
		return serveHTTP(ctx, s, p)
	case assistantwire.MethodTools:
		return map[string]any{"tools": s.Tools(ctx)}, nil
	case assistantwire.MethodTool:
		var p ToolCall
		if err := decode(&p); err != nil {
			return nil, err
		}
		return s.Tool(ctx, p), nil
	case assistantwire.MethodContext:
		var p ContextParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return s.Context(ctx, p), nil
	case assistantwire.MethodAction:
		var p Action
		if err := decode(&p); err != nil {
			return nil, err
		}
		return s.Action(ctx, p)
	case assistantwire.MethodTasks:
		return map[string]any{"tasks": s.ActiveTasks(ctx)}, nil
	case assistantwire.MethodCardSent:
		var p CardSentParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		sent, err := s.CardSent(ctx, p)
		return map[string]any{"sent": sent}, err
	case assistantwire.MethodCardsSent:
		var p CardsSentParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		s.CardsSent(ctx, p)
		return map[string]any{}, nil
	case assistantwire.MethodTodoHandoff:
		var p TodoHandoff
		if err := decode(&p); err != nil {
			return nil, err
		}
		return s.TodoHandoff(ctx, p)
	case assistantwire.MethodTodoLink:
		var p TodoLinkParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return map[string]any{}, s.TodoLink(ctx, p)
	case assistantwire.MethodStart:
		var p StartParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		sess, err := s.Start(ctx, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"session": sess}, nil
	case assistantwire.MethodSendParams:
		var p SendParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		params, err := s.SendParams(ctx, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"params": params}, nil
	case assistantwire.MethodRename:
		var p RenameParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return s.Rename(ctx, p)
	}
	if out, ok, err := s.kbRequest(ctx, method, params); ok { // the knowledge base's (kb_bus.go)
		return out, err
	}
	return nil, &rpc.Error{Code: bus.CodeUnknownMethod, Message: "unknown method " + method}
}

// serveHTTP answers an app's request the hub relayed (http.request) with s's routes.
func serveHTTP(ctx context.Context, s *Service, p assistantwire.HTTPRequest) (assistantwire.HTTPResponse, error) {
	target := p.Path
	if p.Query != "" {
		target += "?" + p.Query
	}
	r, err := http.NewRequestWithContext(ctx, p.Method, target, bytes.NewReader(p.Body))
	if err != nil {
		return assistantwire.HTTPResponse{}, &rpc.Error{Code: bus.CodeInvalidParams, Message: err.Error()}
	}
	r.Header = http.Header(p.Header)
	if r.Header == nil {
		r.Header = http.Header{}
	}
	r.ContentLength = int64(len(p.Body))
	w := &recorder{header: http.Header{}}
	s.ServeHTTP(w, r)
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return assistantwire.HTTPResponse{Status: w.status, Header: w.header, Body: w.body.Bytes()}, nil
}

// recorder keeps a response to send back whole.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *recorder) Header() http.Header { return w.header }
func (w *recorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}
func (w *recorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

// Settle returns once the events received so far are taken (a turn's end may still be running: TurnEnded is its
// own). Tests call it after a round trip to the hub, which comes after the events it sent before.
func (b *BusHub) Settle() {
	for b.pending.Load() > 0 {
		time.Sleep(5 * time.Millisecond)
	}
}

// takeEvents takes the hub's events one at a time, in order. A turn's end runs on its own: naming a conversation
// asks a model and a task waits for its agents to settle, and the events after it must not wait for that.
func (b *BusHub) takeEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-b.events:
			if err := b.event(ctx, ev); err != nil {
				b.opts.Logf("assistant: %s: %v", ev.name, err)
			}
			b.pending.Add(-1)
		}
	}
}

func (b *BusHub) event(ctx context.Context, ev busEvent) error {
	s := b.service()
	decode := func(v any) error { return json.Unmarshal(ev.params, v) }
	switch ev.name {
	case assistantwire.EventUserSpoke:
		var p SessionRef
		if err := decode(&p); err != nil {
			return err
		}
		s.UserSpoke(ctx, p)
	case assistantwire.EventTurnStarted:
		var p SessionRef
		if err := decode(&p); err != nil {
			return err
		}
		s.TurnStarted(ctx, p)
	case assistantwire.EventTurnEnded:
		var p SessionRef
		if err := decode(&p); err != nil {
			return err
		}
		go s.TurnEnded(ctx, p)
	case assistantwire.EventSessionsChanged:
		var p DeviceSessions
		if err := decode(&p); err != nil {
			return err
		}
		s.SessionsChanged(ctx, p)
	case assistantwire.EventInteractionResolved:
		var p InteractionResolved
		if err := decode(&p); err != nil {
			return err
		}
		s.InteractionResolved(ctx, p)
	case assistantwire.EventInteractionRaised:
		var p InteractionRaised
		if err := decode(&p); err != nil {
			return err
		}
		s.InteractionRaised(ctx, p)
	case assistantwire.EventInboxMessage:
		var p InboxMessage
		if err := decode(&p); err != nil {
			return err
		}
		s.InboxMessage(ctx, p)
	case assistantwire.EventUpdated:
		var h Hello
		if err := decode(&h); err != nil {
			return err
		}
		b.setHello(h)
		s.ProfileChanged(ctx) // the live context and the settings are read again
		s.Resync(ctx)         // and the hub gets the profile in effect now (the default machine may be another)
	case assistantwire.EventResync:
		go s.Resync(ctx)
	case assistantwire.EventTodoSpaceRenamed:
		var p TodoSpaceRenamed
		if err := decode(&p); err != nil {
			return err
		}
		s.TodoSpaceRenamed(ctx, p)
	case "managers": // the bus's roster: nothing the assistant needs
	default:
		if ok, err := s.kbEvent(ctx, ev.name, ev.params); ok { // the knowledge base's (kb_bus.go)
			return err
		}
		return fmt.Errorf("unknown event")
	}
	return nil
}
