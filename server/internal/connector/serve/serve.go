// Package serve runs a connector in a program of its own, for the hub (connector/proc): the hub starts the program
// and talks to it over its stdin / stdout with the hub↔node JSON-lines RPC (internal/rpc). One connector per program.
// A connector whose dependencies would make jarvisd much bigger lives this way (jarvisd-telegram, docs/plan-telegram.md).
//
// The hub calls the methods below; the program calls back for its secret (secrets.*: the hub binds them to this
// program's connector) and tells the hub what changed (the changed event). Nothing here logs what crosses the pipe.
// A write (actions.do) comes only once the hub's gate let it through; the program never starts one itself.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Methods the hub calls.
const (
	MInit      = "init" // InitParams → Manifest: build the connector and start it
	MManifest  = "manifest"
	MStatus    = "status"
	MLoginStep = "login.step"
	MLogout    = "logout"
	MChanges   = "archive.changes"
	MCollect   = "inbox.collect"
	MReply     = "inbox.reply"
	MTools     = "tools.list"
	MCall      = "tools.call"
	MActions   = "actions.list"
	MDescribe  = "actions.describe"
	MDo        = "actions.do" // only once the hub's gate let it through
)

// Methods the program calls on the hub, and its event.
const (
	MSecretsLoad   = "secrets.load"
	MSecretsSave   = "secrets.save"
	MSecretsDelete = "secrets.delete"
	EChanged       = "changed" // ChangedEvent
)

// Protocol is the version of this protocol (docs/connector-protocol.md): the hub sends it with init, the program
// answers with its own in the manifest, and the hub refuses a program of another version.
const Protocol = 2

// Manifest is who the connector is and its connector.Manifest, on the wire.
type Manifest struct {
	Protocol     int      `json:"protocol"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Hosts        []string `json:"hosts,omitempty"`
	AgentTools   []string `json:"agent_tools,omitempty"`
	Unlisted     []string `json:"unlisted,omitempty"`
	PollSeconds  int      `json:"poll_seconds,omitempty"`
	Kinds        []string `json:"kinds,omitempty"`
}

type (
	InitParams struct {
		Protocol int             `json:"protocol"`
		Config   json.RawMessage `json:"config,omitempty"`
	}
	LoginParams struct {
		Step   string            `json:"step,omitempty"`
		Values map[string]string `json:"values,omitempty"`
	}
	ChangesParams struct {
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	ChangesResult struct {
		Batches []connector.ChatBatch `json:"batches,omitempty"`
		Next    string                `json:"next,omitempty"`
		More    bool                  `json:"more,omitempty"`
	}
	CollectResult struct {
		Things []connector.InboxThing `json:"things,omitempty"`
	}
	// ReplyParams is the reply as the connector gets it.
	ReplyParams = connector.InboxReply
	ToolsResult struct {
		Tools []proto.Tool `json:"tools,omitempty"`
	}
	CallParams struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args,omitempty"`
	}
	CallResult struct {
		Text string `json:"text,omitempty"`
	}
	// DescribeResult is what the card says about a write (connector.Actions.Describe).
	DescribeResult struct {
		Action  connector.Action `json:"action"`
		Summary string           `json:"summary"`
	}
	// DoParams is a write the gate let through: the very args the card was made from, and the hub's id for this one
	// run, the same if the hub ever sends it again (the program runs it at most once).
	DoParams struct {
		Name      string          `json:"name"`
		Args      json.RawMessage `json:"args,omitempty"`
		RequestID string          `json:"request_id"`
	}
	SecretData struct {
		Data []byte `json:"data,omitempty"` // nil: nothing stored
	}
	ChangedEvent struct {
		Scope string `json:"scope"`
	}
)

// Error codes a program answers with (rpc.Error.Code), for callers that act on the kind of failure.
const (
	CodeNotInitialized = "not_initialized"
	CodeNoCapability   = "no_capability"
	CodeUnknownMethod  = "unknown_method"
	CodeWrongProtocol  = "wrong_protocol"
)

// Build makes the connector once the hub has sent its configuration (hub.json's section for it); secrets is the
// hub's store for this connector.
type Build func(config json.RawMessage, secrets connector.Secrets) (connector.Connector, error)

// Restarts of a failing Watch inside the program (the hub's Registry restarts the hub side).
var (
	watchFirst = 5 * time.Second
	watchLast  = 5 * time.Minute
)

// Main serves build's connector on in / out until the hub closes them (the program then exits) or ctx ends. What the
// connector logs goes to the standard logger (the program's stderr, which the hub keeps).
func Main(ctx context.Context, in io.Reader, out io.Writer, build Build) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	peer := rpc.New("c", out)
	s := &server{peer: peer, build: build, ctx: ctx}
	peer.OnRequest = s.handle
	return peer.Serve(ctx, in)
}

// Serve serves c, built and running already (its Run and Watch are the caller's), on in / out: what a test holds a
// connector to the protocol with (connector/conformance). Its secrets are its own.
func Serve(ctx context.Context, in io.Reader, out io.Writer, c connector.Connector) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	peer := rpc.New("c", out)
	s := &server{peer: peer, ctx: ctx, c: c, running: true}
	peer.OnRequest = s.handle
	return peer.Serve(ctx, in)
}

type server struct {
	peer  *rpc.Peer
	build Build
	ctx   context.Context
	// running: c was handed over built and running (Serve); init only answers its manifest.
	running bool

	mu sync.Mutex
	c  connector.Connector
	// once runs each actions.do once by its request id.
	once writes
}

func (s *server) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == MInit {
		var p InitParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if p.Protocol != Protocol {
			return nil, &rpc.Error{Code: CodeWrongProtocol, Message: fmt.Sprintf("protocol %d, not %d", p.Protocol, Protocol)}
		}
		return s.init(p.Config)
	}
	s.mu.Lock()
	c := s.c
	s.mu.Unlock()
	if c == nil {
		return nil, &rpc.Error{Code: CodeNotInitialized, Message: "not initialized"}
	}
	switch method {
	case MManifest:
		return ManifestOf(c), nil
	case MStatus:
		return connector.StatusOf(ctx, c), nil
	case MLoginStep:
		l, ok := connector.As[connector.Login](c)
		if !ok {
			return nil, noCapability(c, "login")
		}
		var p LoginParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return l.LoginStep(ctx, p.Step, p.Values)
	case MLogout:
		l, ok := connector.As[connector.Login](c)
		if !ok {
			return nil, noCapability(c, "login")
		}
		return struct{}{}, l.Logout(ctx)
	case MChanges:
		a, ok := connector.As[connector.Archive](c)
		if !ok {
			return nil, noCapability(c, "archive")
		}
		var p ChangesParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		b, next, more, err := a.Changes(ctx, p.Cursor, p.Limit)
		return ChangesResult{Batches: b, Next: next, More: more}, err
	case MCollect:
		in, ok := connector.As[connector.Inbox](c)
		if !ok {
			return nil, noCapability(c, "inbox")
		}
		things, err := in.Collect(ctx)
		return CollectResult{Things: things}, err
	case MReply:
		in, ok := connector.As[connector.Inbox](c)
		if !ok {
			return nil, noCapability(c, "inbox")
		}
		var p ReplyParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return struct{}{}, in.Reply(ctx, p)
	case MTools:
		t, ok := connector.As[connector.ReadTools](c)
		if !ok {
			return ToolsResult{}, nil
		}
		return ToolsResult{Tools: t.Tools()}, nil
	case MCall:
		t, ok := connector.As[connector.ReadTools](c)
		if !ok {
			return nil, noCapability(c, "tools")
		}
		var p CallParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		text, err := t.Call(ctx, p.Name, p.Args)
		return CallResult{Text: text}, err
	case MActions:
		a, ok := connector.As[connector.Actions](c)
		if !ok {
			return ToolsResult{}, nil
		}
		return ToolsResult{Tools: a.Actions()}, nil
	case MDescribe:
		a, ok := connector.As[connector.Actions](c)
		if !ok {
			return nil, noCapability(c, "actions")
		}
		var p CallParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		act, summary, err := a.Describe(ctx, p.Name, p.Args)
		return DescribeResult{Action: act, Summary: summary}, err
	case MDo:
		a, ok := connector.As[connector.Actions](c)
		if !ok {
			return nil, noCapability(c, "actions")
		}
		var p DoParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		if p.RequestID == "" {
			return nil, errors.New("actions.do without a request_id")
		}
		return s.once.do(p.RequestID, func() (CallResult, error) {
			text, err := a.Do(ctx, p.Name, p.Args)
			return CallResult{Text: text}, err
		})
	}
	return nil, &rpc.Error{Code: CodeUnknownMethod, Message: "unknown method " + method}
}

// init builds the connector the first time and starts its Run and Watch; a second init answers the same.
func (s *server) init(config json.RawMessage) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		if s.running {
			return ManifestOf(s.c), connector.Check(s.c)
		}
		return ManifestOf(s.c), nil
	}
	c, err := s.build(config, secrets{s.peer})
	if err != nil {
		return Manifest{}, err
	}
	if err := connector.Check(c); err != nil {
		return Manifest{}, err
	}
	s.c = c
	if r, ok := c.(connector.Runner); ok {
		go r.Run(s.ctx)
	}
	if e, ok := c.(connector.Events); ok {
		go s.watch(e)
	}
	return ManifestOf(c), nil
}

// watch passes the connector's changes on to the hub, starting a Watch that fails again after a backoff.
func (s *server) watch(e connector.Events) {
	wait := watchFirst
	for s.ctx.Err() == nil {
		start := time.Now()
		err := e.Watch(s.ctx, func(scope connector.Scope) { _ = s.peer.Notify(EChanged, ChangedEvent{Scope: string(scope)}) })
		if s.ctx.Err() != nil {
			return
		}
		if time.Since(start) > time.Minute {
			wait = watchFirst
		}
		log.Printf("watch stopped (%v), again in %s", err, wait)
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, watchLast)
	}
}

// doneMost is how many writes the program remembers by request id.
const doneMost = 256

// writes runs each write (actions.do) once by its request id: the same id again answers what the first run did,
// waiting for it if it is still running.
type writes struct {
	mu    sync.Mutex
	runs  map[string]*write
	order []string
}

type write struct {
	done chan struct{}
	res  CallResult
	err  error
}

func (w *writes) do(id string, run func() (CallResult, error)) (CallResult, error) {
	w.mu.Lock()
	if prev, ok := w.runs[id]; ok {
		w.mu.Unlock()
		<-prev.done
		return prev.res, prev.err
	}
	if w.runs == nil {
		w.runs = map[string]*write{}
	}
	cur := &write{done: make(chan struct{})}
	w.runs[id] = cur
	w.order = append(w.order, id)
	if len(w.order) > doneMost {
		delete(w.runs, w.order[0])
		w.order = w.order[1:]
	}
	w.mu.Unlock()
	cur.res, cur.err = run()
	close(cur.done)
	return cur.res, cur.err
}

// ManifestOf is c's manifest on the wire.
func ManifestOf(c connector.Connector) Manifest {
	cm := c.Manifest()
	m := Manifest{Protocol: Protocol, ID: c.ID(), Name: c.Name(), Capabilities: []string{}, Hosts: cm.Hosts, AgentTools: cm.AgentTools,
		Unlisted: cm.Unlisted, PollSeconds: int(cm.Poll / time.Second), Kinds: cm.Kinds}
	for _, cp := range cm.Capabilities {
		m.Capabilities = append(m.Capabilities, string(cp))
	}
	return m
}

// Of is the connector.Manifest a program sent (a proxy's: it answers for the program).
func (m Manifest) Of() connector.Manifest {
	out := connector.Manifest{Hosts: m.Hosts, AgentTools: m.AgentTools, Unlisted: m.Unlisted, Poll: time.Duration(m.PollSeconds) * time.Second,
		Kinds: m.Kinds, Proxy: true}
	for _, cp := range m.Capabilities {
		out.Capabilities = append(out.Capabilities, connector.Capability(cp))
	}
	return out
}

func noCapability(c connector.Connector, what string) error {
	return &rpc.Error{Code: CodeNoCapability, Message: fmt.Sprintf("%s has no %s", c.Name(), what)}
}

// secrets is the hub's store for this program's connector, over the pipe.
type secrets struct{ peer *rpc.Peer }

func (s secrets) Load(ctx context.Context) ([]byte, error) {
	var out SecretData
	err := s.peer.Call(ctx, MSecretsLoad, struct{}{}, &out)
	return out.Data, err
}

func (s secrets) Save(ctx context.Context, data []byte) error {
	return s.peer.Call(ctx, MSecretsSave, SecretData{Data: data}, nil)
}

func (s secrets) Delete(ctx context.Context) error {
	return s.peer.Call(ctx, MSecretsDelete, struct{}{}, nil)
}
