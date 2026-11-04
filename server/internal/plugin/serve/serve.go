// Package serve is a plugin program's side of the plugin protocol (docs/plugins.md「进程插件协议」): it answers
// initialize and ping and hands each namespaced call (connector.status, …) to the handler of its namespace. A
// third party's program written in Go can use it as is; one in another language does the same on the wire and is
// held to it by plugin/conformance.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Handler answers the calls of one namespace: method is without the namespace ("status" for connector.status),
// params as sent (with "contribution").
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// Program is a plugin program.
type Program struct {
	ID, Version string
	// Manifest is its persona-plugin.json, answered to manifest (a plugin elsewhere must have it; one the hub starts
	// from its package may leave it out).
	Manifest json.RawMessage
	// Init is called once, with what the hub sent; it says what the program contributes this run (a subset of its
	// manifest's), and may start the program's own work with ctx (which ends with the program).
	Init func(ctx context.Context, p plugin.InitializeParams, hub *Hub) (contributes map[string]any, err error)
	// Namespaces are the handlers by namespace ("connector").
	Namespaces map[string]Handler
}

// Hub is the hub, for a program's calls and events.
type Hub struct{ peer *rpc.Peer }

// Call calls a method of the hub's (connector.secrets.load, …).
func (h *Hub) Call(ctx context.Context, method string, params, out any) error {
	return h.peer.Call(ctx, method, params, out)
}

// Notify sends the hub an event (connector.changed, …).
func (h *Hub) Notify(event string, params any) error { return h.peer.Notify(event, params) }

// Serve serves prog on in / out until the hub closes them (the program should then exit) or ctx ends.
func Serve(ctx context.Context, in io.Reader, out io.Writer, prog Program) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	peer := rpc.New("p", out)
	s := &server{prog: prog, hub: &Hub{peer: peer}, ctx: ctx}
	peer.OnRequest = s.handle
	peer.OnEvent = s.event
	return peer.Serve(ctx, in)
}

type server struct {
	prog Program
	hub  *Hub
	ctx  context.Context

	mu          sync.Mutex
	initialized bool
	contributes map[string]any
}

func (s *server) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == plugin.MInitialize {
		return s.initialize(params)
	}
	if method == plugin.MManifest {
		if len(s.prog.Manifest) == 0 {
			return nil, &rpc.Error{Code: plugin.CodeUnknownMethod, Message: "no manifest here: it is in the package"}
		}
		return s.prog.Manifest, nil
	}
	s.mu.Lock()
	ok := s.initialized
	s.mu.Unlock()
	if !ok {
		return nil, &rpc.Error{Code: plugin.CodeNotInitialized, Message: "not initialized"}
	}
	if method == plugin.MPing {
		return struct{}{}, nil
	}
	ns, rest, found := strings.Cut(method, ".")
	h := s.prog.Namespaces[ns]
	if !found || h == nil {
		return nil, &rpc.Error{Code: plugin.CodeUnknownMethod, Message: "unknown method " + method}
	}
	return h(ctx, rest, params)
}

// event hands an event the hub tells (event.<topic>) to its namespace's handler; nothing is answered.
func (s *server) event(method string, params json.RawMessage) {
	s.mu.Lock()
	ok := s.initialized
	s.mu.Unlock()
	ns, rest, found := strings.Cut(method, ".")
	if h := s.prog.Namespaces[ns]; ok && found && h != nil {
		go func() { _, _ = h(s.ctx, rest, params) }()
	}
}

func (s *server) initialize(raw json.RawMessage) (any, error) {
	var p plugin.InitializeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Protocol != plugin.Protocol {
		return nil, &rpc.Error{Code: plugin.CodeWrongProtocol, Message: fmt.Sprintf("protocol %d, not %d", p.Protocol, plugin.Protocol)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		var contributes map[string]any
		if s.prog.Init != nil {
			var err error
			if contributes, err = s.prog.Init(s.ctx, p, s.hub); err != nil {
				return nil, err
			}
		}
		s.initialized, s.contributes = true, contributes
	}
	res := plugin.InitializeResult{Protocol: plugin.Protocol, ID: s.prog.ID, Version: s.prog.Version}
	if len(s.contributes) > 0 {
		res.Contributes = map[string]json.RawMessage{}
		for k, v := range s.contributes {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			res.Contributes[k] = b
		}
	}
	return res, nil
}

// NoContribution is the error for a call to a contribution the program does not have.
func NoContribution(id string) error {
	return &rpc.Error{Code: plugin.CodeNoContribution, Message: "no contribution " + id}
}

// Contribution reads the contribution a call is for ("" when the call did not say).
func Contribution(params json.RawMessage) string {
	var t plugin.Target
	_ = json.Unmarshal(params, &t)
	return t.Contribution
}

// UnknownMethod is the error for a method the namespace does not have.
func UnknownMethod(method string) error {
	return &rpc.Error{Code: plugin.CodeUnknownMethod, Message: "unknown method " + method}
}
