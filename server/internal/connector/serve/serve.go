// Package serve runs a connector in a program of its own, for the hub (connector/proc): the hub starts the program
// and talks to it over its stdin / stdout with the hub↔node JSON-lines RPC (internal/rpc). One connector per program.
// A connector whose dependencies would make jarvisd much bigger lives this way (jarvisd-telegram, docs/plan-telegram.md).
//
// The hub calls the methods below; the program calls back for its secret (secrets.*: the hub binds them to this
// program's connector) and tells the hub what changed (the changed event). Nothing here logs what crosses the pipe.
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

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
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
)

// Methods the program calls on the hub, and its event.
const (
	MSecretsLoad   = "secrets.load"
	MSecretsSave   = "secrets.save"
	MSecretsDelete = "secrets.delete"
	EChanged       = "changed" // ChangedEvent
)

// Manifest is who the connector is and what it can do (connector.Capabilities' names).
type Manifest struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Hosts        []string `json:"hosts,omitempty"`
}

type (
	InitParams  struct{ Config json.RawMessage }
	LoginParams struct {
		Step   string
		Values map[string]string
	}
	ChangesParams struct {
		Cursor string
		Limit  int
	}
	ChangesResult struct {
		Batches []connector.ChatBatch
		Next    string
		More    bool
	}
	CollectResult struct{ Things []connector.InboxThing }
	ReplyParams   struct{ Thread, Text string }
	ToolsResult   struct{ Tools []proto.Tool }
	CallParams    struct {
		Name string
		Args json.RawMessage
	}
	CallResult   struct{ Text string }
	SecretData   struct{ Data []byte } // nil: nothing stored
	ChangedEvent struct{ Scope string }
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

type server struct {
	peer  *rpc.Peer
	build Build
	ctx   context.Context

	mu sync.Mutex
	c  connector.Connector
}

func (s *server) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if method == MInit {
		var p InitParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return s.init(p.Config)
	}
	s.mu.Lock()
	c := s.c
	s.mu.Unlock()
	if c == nil {
		return nil, errors.New("not initialized")
	}
	switch method {
	case MManifest:
		return ManifestOf(c), nil
	case MStatus:
		return connector.StatusOf(ctx, c), nil
	case MLoginStep:
		l, ok := c.(connector.Login)
		if !ok {
			return nil, noCapability(c, "login")
		}
		var p LoginParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return l.LoginStep(ctx, p.Step, p.Values)
	case MLogout:
		l, ok := c.(connector.Login)
		if !ok {
			return nil, noCapability(c, "login")
		}
		return struct{}{}, l.Logout(ctx)
	case MChanges:
		a, ok := c.(connector.Archive)
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
		in, ok := c.(connector.Inbox)
		if !ok {
			return nil, noCapability(c, "inbox")
		}
		things, err := in.Collect(ctx)
		return CollectResult{Things: things}, err
	case MReply:
		in, ok := c.(connector.Inbox)
		if !ok {
			return nil, noCapability(c, "inbox")
		}
		var p ReplyParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return struct{}{}, in.Reply(ctx, p.Thread, p.Text)
	case MTools:
		t, ok := c.(connector.ReadTools)
		if !ok {
			return ToolsResult{}, nil
		}
		return ToolsResult{Tools: t.Tools()}, nil
	case MCall:
		t, ok := c.(connector.ReadTools)
		if !ok {
			return nil, noCapability(c, "tools")
		}
		var p CallParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		text, err := t.Call(ctx, p.Name, p.Args)
		return CallResult{Text: text}, err
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// init builds the connector the first time and starts its Run and Watch; a second init answers the same.
func (s *server) init(config json.RawMessage) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		return ManifestOf(s.c), nil
	}
	c, err := s.build(config, secrets{s.peer})
	if err != nil {
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
		err := e.Watch(s.ctx, func(scope string) { _ = s.peer.Notify(EChanged, ChangedEvent{Scope: scope}) })
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

// ManifestOf is c's manifest.
func ManifestOf(c connector.Connector) Manifest {
	m := Manifest{ID: c.ID(), Name: c.Name(), Capabilities: connector.Capabilities(c)}
	if h, ok := c.(connector.Hosts); ok {
		m.Hosts = h.Hosts()
	}
	return m
}

func noCapability(c connector.Connector, what string) error {
	return fmt.Errorf("%s has no %s", c.Name(), what)
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
