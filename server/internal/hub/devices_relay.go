package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/link"
)

// The relay to the manager (devicewire.RelayPath): connections the hub holds for it — a Persona Link and the streams
// it opens (GET /api/v1/link, /api/v1/link/stream: the computer dials the hub's address, as before the move), the
// center's stream to one of this hub's computers (peer_node.go) — and the node streams the manager opens through
// another hub of the mesh. For each, the manager dials a WebSocket with its bus token and the hub copies messages both
// ways as they are (text: Persona Link's messages; binary: a byte stream); a close code and reason go through. The
// hub checks the credential, keeps the heartbeat with Persona Link (its side of the connection) and closes a link
// whose credential is revoked; what the link says is the manager's.

const kindLink = "link"

// relayWait is how long the hub waits for the manager to take a connection it holds for it.
const relayWait = 10 * time.Second

// linkManagerWait: how long a Persona Link (or a center's node stream) waits for the manager while the hub starts,
// short of Persona Link's handshake timeout (8 s a candidate).
const linkManagerWait = 5 * time.Second

// relayState is the connections waiting for the manager, and the Persona Links relayed now.
type relayState struct {
	mu      sync.Mutex
	pending map[string]chan *relayTaken
	links   map[*relayLink]struct{}
}

// relayTaken is the manager's side of a relay, once it dialed; done closes when the hub is through with it.
type relayTaken struct {
	id   string // devices.accept's
	conn *websocket.Conn
	done chan struct{}
}

// relayLink is a Persona Link connection (or one of its streams) the hub relays.
type relayLink struct {
	conn    *websocket.Conn // Persona Link's side
	tokenID string
	stream  bool
}

func (h *Hub) linkRoutes(api *http.ServeMux) {
	api.HandleFunc("GET "+link.Path, h.serveLink)
	api.HandleFunc("GET "+link.StreamPath, h.serveLinkStream)
}

// relayRoutes mounts the manager's relay channel beside the bus (its token, not a device's).
func (h *Hub) relayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+devicewire.RelayPath, h.serveRelay)
}

// linkCaller checks a link request: a Persona Link credential, for a machine of kind link, over TLS.
func (h *Hub) linkCaller(w http.ResponseWriter, r *http.Request) (principal, bool) {
	p := principalOf(r.Context())
	if p.Role != roleLink {
		writeErr(w, http.StatusForbidden, errors.New("only a Persona Link credential opens a machine link"))
		return p, false
	}
	if r.TLS == nil && h.sourceOf(r) != sourceLoopback {
		h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
		return p, false
	}
	if mc := h.cfgEntry(p.Machine); mc == nil || mc.Kind != kindLink {
		writeErr(w, http.StatusNotFound, fmt.Errorf("hub.json has no machine %q of kind link", p.Machine))
		return p, false
	}
	return p, true
}

// enterWS counts a WebSocket handler for the shutdown (stopWebSockets), unless the hub is already stopping.
func (h *Hub) enterWS() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsStopping {
		return false
	}
	h.wsHandlers.Add(1)
	return true
}

func (h *Hub) serveLink(w http.ResponseWriter, r *http.Request) {
	p, ok := h.linkCaller(w, r)
	if !ok {
		return
	}
	h.relayLinkConn(w, r, devicewire.Accept{Kind: devicewire.AcceptLink, MachineID: p.Machine, TokenID: p.ID, Source: maskedSource(r)},
		&websocket.AcceptOptions{InsecureSkipVerify: true}, 1<<20)
}

func (h *Hub) serveLinkStream(w http.ResponseWriter, r *http.Request) {
	p, ok := h.linkCaller(w, r)
	if !ok {
		return
	}
	// Compressed like the ssh connections (Compression=yes): node traffic is JSON.
	h.relayLinkConn(w, r, devicewire.Accept{Kind: devicewire.AcceptLinkStream, MachineID: p.Machine, TokenID: p.ID,
		StreamID: r.URL.Query().Get("id")}, &websocket.AcceptOptions{InsecureSkipVerify: true,
		CompressionMode: websocket.CompressionContextTakeover}, 64<<20)
}

// relayLinkConn takes Persona Link's connection and hands it to the manager: 503 assistant_offline while there is none
// (Persona Link tries again).
func (h *Hub) relayLinkConn(w http.ResponseWriter, r *http.Request, a devicewire.Accept, opts *websocket.AcceptOptions, limit int64) {
	// Right after the hub started (its listeners come up before the manager does) the link waits for the manager a
	// little, within what Persona Link gives a handshake; otherwise it is told to come back.
	if !h.awaitManager(r.Context(), linkManagerWait) {
		writeErrCode(w, http.StatusServiceUnavailable, "assistant_offline", errAssistantOffline)
		return
	}
	if !h.enterWS() {
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	defer h.wsHandlers.Done()
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	conn.SetReadLimit(limit)
	l := &relayLink{conn: conn, tokenID: a.TokenID, stream: a.Kind == devicewire.AcceptLinkStream}
	rs := &h.relay
	rs.mu.Lock()
	if h.wsStopping {
		rs.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "hub stopping")
		return
	}
	if rs.links == nil {
		rs.links = map[*relayLink]struct{}{}
	}
	rs.links[l] = struct{}{}
	rs.mu.Unlock()
	defer func() {
		rs.mu.Lock()
		delete(rs.links, l)
		rs.mu.Unlock()
	}()
	taken, err := h.handToManager(r.Context(), a)
	if err != nil {
		_ = conn.Close(websocket.StatusTryAgainLater, clip(errAssistantOffline.Error(), 100))
		return
	}
	defer close(taken.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	every, timeout := h.linkPingTimes()
	var why atomic.Value // why the hub ended it, for the manager
	go func() {          // a link whose network went away quietly (a laptop gone to sleep): no pong
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, timeout)
				start := time.Now()
				err := conn.Ping(pctx)
				pcancel()
				if err != nil && ctx.Err() == nil {
					why.Store(fmt.Sprintf("no pong within %s", timeout))
					_ = conn.CloseNow()
					return
				}
				if err == nil && !l.stream { // the link's round trip, for the manager's choice of paths
					_ = h.notifyBus(devicesOwner, devicewire.EventLinkRTT, devicewire.LinkRTT{ID: taken.id,
						RTTMS: max(1, int(time.Since(start).Milliseconds()))})
				}
			}
		}
	}()
	spliceWS(conn, taken.conn, func() string { s, _ := why.Load().(string); return s })
}

// handToManager has the manager take a connection the hub holds: it dials the relay with the id and the hub waits
// for it.
func (h *Hub) handToManager(ctx context.Context, a devicewire.Accept) (*relayTaken, error) {
	rs := &h.relay
	a.ID = randomHex(12)
	ch := make(chan *relayTaken, 1)
	rs.mu.Lock()
	if rs.pending == nil {
		rs.pending = map[string]chan *relayTaken{}
	}
	rs.pending[a.ID] = ch
	rs.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, relayWait)
	defer cancel()
	err := h.callBus(cctx, devicesOwner, devicewire.MethodAccept, a, nil)
	if err == nil {
		select {
		case t := <-ch:
			return t, nil
		case <-cctx.Done():
			err = errors.New("the manager did not take the connection in time")
		}
	}
	// Not wanted any more; one the manager dialed just now goes away (serveRelay delivers under rs.mu, and the close
	// handshake is not waited for under it).
	rs.mu.Lock()
	delete(rs.pending, a.ID)
	var late *relayTaken
	select {
	case late = <-ch:
	default:
	}
	rs.mu.Unlock()
	if late != nil {
		go func() {
			_ = late.conn.Close(websocket.StatusGoingAway, "not wanted any more")
			close(late.done)
		}()
	}
	return nil, err
}

// serveRelay is the manager's side of a relay: one the hub holds for it (?id=), or a node stream through another hub
// of the mesh (?hub=&machine=).
func (h *Hub) serveRelay(w http.ResponseWriter, r *http.Request) {
	ident, ok := h.busCaller(w, r)
	if !ok {
		return
	}
	if ident.ID != devicesOwner {
		writeErr(w, http.StatusForbidden, errors.New("only the manager that carries the machines relays them"))
		return
	}
	if !h.enterWS() {
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	defer h.wsHandlers.Done()
	q := r.URL.Query()
	if hub := q.Get("hub"); hub != "" {
		h.relayThroughHub(w, r, hub, q.Get("machine"))
		return
	}
	rs := &h.relay
	id := q.Get("id")
	rs.mu.Lock()
	ch := rs.pending[id]
	rs.mu.Unlock()
	if ch == nil {
		writeErr(w, http.StatusNotFound, errors.New("no such relay"))
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(64 << 20)
	t := &relayTaken{id: id, conn: conn, done: make(chan struct{})}
	rs.mu.Lock()
	delivered := rs.pending[id] == ch
	if delivered {
		delete(rs.pending, id)
		ch <- t // buffered: one delivery per id
	}
	rs.mu.Unlock()
	if !delivered {
		_ = conn.Close(websocket.StatusGoingAway, "not wanted any more")
		return
	}
	<-t.done
}

// relayThroughHub opens a node stream through hub of the mesh for the manager and copies it to the relay.
func (h *Hub) relayThroughHub(w http.ResponseWriter, r *http.Request, hub, mid string) {
	octx, ocancel := context.WithCancel(context.Background())
	defer ocancel()
	pipe, err := h.openThroughHub(octx, hub, mid)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer pipe.close()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(64 << 20)
	nc := websocket.NetConn(octx, conn, websocket.MessageBinary)
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(pipe.w, nc); done <- struct{}{} }()
	go func() { _, _ = io.Copy(nc, pipe.r); done <- struct{}{} }()
	<-done
	pipe.close()
	_ = nc.Close()
	<-done
}

// spliceWS copies messages between a (the outside's) and b (the manager's) until either ends; the close code and
// reason of one go to the other. why is the hub's own reason when a went away without one (its heartbeat).
func spliceWS(a, b *websocket.Conn, why func() string) {
	done := make(chan struct{}, 2)
	pass := func(from, to *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			typ, data, err := from.Read(context.Background())
			if err != nil {
				code, reason := websocket.CloseStatus(err), ""
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					reason = ce.Reason
				}
				switch code {
				case -1, websocket.StatusNoStatusRcvd, websocket.StatusAbnormalClosure, websocket.StatusTLSHandshake:
					code, reason = websocket.StatusGoingAway, "the other side went away"
					if w := why(); w != "" && from == a {
						reason = w
					}
				}
				_ = to.Close(code, reason)
				return
			}
			wctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			err = to.Write(wctx, typ, data)
			cancel()
			if err != nil {
				_ = from.Close(websocket.StatusGoingAway, "the other side went away")
				return
			}
		}
	}
	go pass(a, b)
	go pass(b, a)
	<-done
	<-done
}

// closeLinks closes the links that came with a revoked credential.
func (h *Hub) closeLinks(tokenID string) int {
	rs := &h.relay
	rs.mu.Lock()
	var gone []*relayLink
	for l := range rs.links {
		if l.tokenID == tokenID {
			gone = append(gone, l)
		}
	}
	rs.mu.Unlock()
	n := 0
	for _, l := range gone {
		if !l.stream {
			n++
		}
		go func() { _ = l.conn.Close(link.CloseRevoked, "credential revoked") }()
	}
	return n
}

// stopLinks is part of the shutdown: every link gets a 1001 so Persona Link comes back to the next hub quickly.
func (h *Hub) stopLinks() []*relayLink {
	rs := &h.relay
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]*relayLink, 0, len(rs.links))
	for l := range rs.links {
		out = append(out, l)
	}
	return out
}

// linkEndpoints is where the hub can be found, and what it is called and how many hubs its mesh has (the Mac menu
// bar shows both, docs/link.md); the manager tells Persona Link.
func (h *Hub) linkEndpoints() link.Message {
	msg := link.Message{Type: "endpoints", HubName: h.hubName(), MeshHubs: h.meshHubCount()}
	for _, e := range h.endpointList() {
		if e.Scheme == "https" {
			msg.Endpoints = append(msg.Endpoints, link.Endpoint{URL: e.URL, Kind: e.Kind})
		}
	}
	return msg
}

// sendLinkEndpoints: the hub's addresses or its mesh changed; the manager tells the links.
func (h *Hub) sendLinkEndpoints() { h.devicesConfigChanged() }
