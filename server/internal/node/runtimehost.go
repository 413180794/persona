package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// RuntimeHost is the runtime socket (runtime.sock, runtimes.go) and the runtimes connected to it. A resident node
// (`jarvisd node --attach`, attach.go) makes one when it starts and shares it with the Node it makes for each hub
// connection, so runtimes connect before the hub does and stay connected while it reconnects: their sessions are in
// the first list the next connection sends, and nothing on the runtime's side notices the gap. A node serving its one
// connection itself (`--stdio` with no resident node) has one of its own, for that connection (listenRuntimeSocket).
//
// Between hub connections runtimes are still answered: hello (with hub: false), their lists are kept, what they push
// for the phone is dropped (the hub re-watches from where the phone is when it is back), and an ask answers node_gone
// so the runtime asks again, with the same id, once a hub is connected. Runtimes are told when the hub comes and goes
// (runtime.hub), so they do not wait on a phone that cannot answer.
type RuntimeHost struct {
	reg     *runtimeRegistry
	path    string
	version string
	logger  *log.Logger

	mu   sync.Mutex
	node *Node // the Node serving a hub connection now; nil between connections
}

// NewRuntimeHost is a runtime socket at path (RuntimeSocketPath when empty) for a resident node; Attach starts it.
func NewRuntimeHost(version, path string) *RuntimeHost {
	if path == "" {
		path = RuntimeSocketPath()
	}
	return &RuntimeHost{reg: newRuntimeRegistry(), path: path, version: version,
		logger: log.New(os.Stderr, "node: ", log.LstdFlags|log.Lmsgprefix)}
}

// current is the Node serving the hub now, nil between connections.
func (rh *RuntimeHost) current() *Node {
	rh.mu.Lock()
	defer rh.mu.Unlock()
	return rh.node
}

// attach makes n the Node serving the hub: it lists and drives the runtimes' sessions from now on. What the last hub
// connection watched is forgotten (the new one watches afresh); runtimes hear that a hub is there.
func (rh *RuntimeHost) attach(n *Node) {
	rh.mu.Lock()
	rh.node = n
	rh.mu.Unlock()
	n.runtimes = rh.reg
	n.runtimeHost = rh
	rh.reg.resetWatches()
	rh.notifyHub(true)
}

// detach: n no longer serves the hub (its connection ended). A newer Node may have attached already: then nothing.
func (rh *RuntimeHost) detach(n *Node) {
	rh.mu.Lock()
	if rh.node != n {
		rh.mu.Unlock()
		return
	}
	rh.node = nil
	rh.mu.Unlock()
	rh.reg.resetWatches()
	rh.notifyHub(false)
}

// notifyHub tells every connected runtime whether a hub is connected.
func (rh *RuntimeHost) notifyHub(connected bool) {
	for _, h := range rh.reg.ready() {
		_ = h.peer.Notify("runtime.hub", map[string]any{"connected": connected})
	}
}

// Listen serves runtimes on the socket until ctx ends, re-creating the socket file if something removes it (an older
// node shutting down), as the hook socket does.
func (rh *RuntimeHost) Listen(ctx context.Context) error {
	if _, err := rh.listenOnce(ctx); err != nil {
		return err
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
			if _, err := os.Stat(rh.path); os.IsNotExist(err) {
				rh.logger.Printf("runtime socket vanished, re-creating")
				if _, err := rh.listenOnce(ctx); err != nil {
					rh.logger.Printf("runtime socket: %v", err)
				}
			}
		}
	}()
	return nil
}

func (rh *RuntimeHost) listenOnce(ctx context.Context) (net.Listener, error) {
	_ = os.Remove(rh.path)
	ln, err := net.Listen("unix", rh.path)
	if err != nil {
		return nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false) // by then the path may belong to a newer node
	_ = os.Chmod(rh.path, 0o600)
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
			go rh.serve(ctx, c)
		}
	}()
	return ln, nil
}

// serve runs one runtime's connection until it ends; its sessions leave the list with it.
func (rh *RuntimeHost) serve(ctx context.Context, c net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	defer c.Close()
	peer := rpc.New("r", c)
	h := rh.reg.add(peer)
	peer.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return rh.request(ctx, h, method, params)
	}
	peer.OnEvent = func(event string, params json.RawMessage) { rh.event(h, event, params) }
	err := peer.Serve(ctx, c)
	peer.Close(err)
	rh.reg.remove(h)
	if id := h.id(); id != "" {
		rh.logger.Printf("runtime %s #%d left: %v", id, h.serial, err)
	}
	if n := rh.current(); n != nil {
		n.notifySessions()
	}
}

// request answers a runtime's call.
func (rh *RuntimeHost) request(ctx context.Context, h *runtimeConn, method string, params json.RawMessage) (any, error) {
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
		rh.logger.Printf("runtime %s %s #%d connected", hello.Runtime, hello.Version, h.serial)
		return map[string]any{"protocol": RuntimeProtocol, "node_version": rh.version, "hub": rh.current() != nil}, nil
	case "interaction.create":
		n := rh.current()
		if n == nil { // no hub to put it in front of the phone: ask again (same id) once one is connected
			return nil, &rpc.Error{Message: "no hub connected", Code: "node_gone"}
		}
		return n.runtimeAsk(ctx, h, params)
	case "mcp.tools":
		n := rh.current()
		if n == nil || n.peer == nil {
			return map[string]any{"tools": []proto.Tool{}}, nil
		}
		var res json.RawMessage
		err := n.peer.Call(ctx, "mcp.tools", map[string]any{}, &res)
		return res, err
	case "mcp.call":
		n := rh.current()
		if n == nil || n.peer == nil {
			return proto.TextResult("Jarvis hub is not connected", true), nil
		}
		var res json.RawMessage
		err := n.peer.Call(ctx, "mcp.call", json.RawMessage(params), &res)
		return res, err
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// event takes a runtime's push: its list is kept whether or not a hub is connected; the rest goes to the hub, or
// nowhere between connections.
func (rh *RuntimeHost) event(h *runtimeConn, event string, params json.RawMessage) {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready {
		return // hello first
	}
	n := rh.current()
	if event == "runtime.sessions" {
		var p struct {
			Sessions []proto.Session `json:"sessions"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		rewatch := rh.reg.setSessions(h, normalizeRuntimeSessions(h, p.Sessions))
		if n == nil {
			return
		}
		for id, since := range rewatch {
			go n.runtimeWatch(h, id, since)
		}
		n.publish(n.sessionList(context.Background(), false))
		return
	}
	if n != nil {
		n.runtimeEvent(h, event, params)
	}
}
