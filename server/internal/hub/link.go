package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/chyroc/jarvis/server/internal/link"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// jarvis-link (internal/link, docs/link.md): a computer the hub cannot reach — a laptop on whatever network it is on —
// dials the hub instead and keeps a link open. Its machine has kind "link" in hub.json and the hub never runs ssh for
// it. When the link says hello the hub knows the machine is reachable and connects at once, by asking jarvis-link to
// open a stream, which it joins to the computer's resident node; from there it is the node protocol as over ssh
// (Machine.connect). The link going away takes the machine offline right then.

const kindLink = "link"

// linkOpenTimeout is how long the hub waits for jarvis-link to open a stream it asked for (a setting).
const linkOpenTimeout = 15 * time.Second

// hubLink is one machine's live link.
type hubLink struct {
	m       *Machine
	conn    *websocket.Conn
	tokenID string // the credential it came with: revoking it closes the link
	version string // jarvis-link's build
	address string // the hub URL it went to
	since   time.Time
	rtt     atomic.Int64 // the last ping's round trip, ns
	// every, timeout: heartbeats (link.PingEvery, link.PingTimeout unless a test set Hub.linkPing).
	every, timeout time.Duration
	// ctx ends with the link, and every stream on it with it; its cause says why.
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu      sync.Mutex
	pending map[string]chan linkStream // stream id -> the connect waiting for it
}

// linkStream is a stream jarvis-link opened, or why it could not. done closes when the hub is through with it.
type linkStream struct {
	conn *websocket.Conn
	done chan struct{}
	err  error
}

func (h *Hub) linkRoutes(api *http.ServeMux) {
	api.HandleFunc("GET "+link.Path, h.serveLink)
	api.HandleFunc("GET "+link.StreamPath, h.serveLinkStream)
}

// linkCaller checks a link request: a jarvis-link credential, for a machine of kind link, over TLS.
func (h *Hub) linkCaller(w http.ResponseWriter, r *http.Request) (*Machine, principal, bool) {
	p := principalOf(r.Context())
	if p.Role != roleLink {
		writeErr(w, http.StatusForbidden, errors.New("only a jarvis-link credential opens a machine link"))
		return nil, p, false
	}
	if r.TLS == nil && h.sourceOf(r) != sourceLoopback {
		h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
		return nil, p, false
	}
	m := h.cfgMachine(p.Machine)
	if m == nil || m.cfg.Kind != kindLink {
		writeErr(w, http.StatusNotFound, fmt.Errorf("hub.json has no machine %q of kind link", p.Machine))
		return nil, p, false
	}
	return m, p, true
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
	m, p, ok := h.linkCaller(w, r)
	if !ok {
		return
	}
	if !h.enterWS() {
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	defer h.wsHandlers.Done()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	var hello link.Message
	hctx, hcancel := context.WithTimeout(r.Context(), 10*time.Second)
	_, data, err := conn.Read(hctx)
	hcancel()
	if err == nil {
		err = json.Unmarshal(data, &hello)
	}
	if err != nil || hello.Type != "hello" || hello.Machine != m.cfg.ID {
		_ = conn.Close(link.CloseRefused, "expected the hello of machine "+m.cfg.ID)
		return
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	l := &hubLink{m: m, conn: conn, tokenID: p.ID, version: clip(strings.TrimSpace(hello.Version), 80), address: clip(strings.TrimSpace(hello.Address), 200),
		since: time.Now().UTC(), every: link.PingEvery, timeout: link.PingTimeout, ctx: ctx, cancel: cancel,
		pending: map[string]chan linkStream{}}
	if h.linkPing[0] > 0 {
		l.every, l.timeout = h.linkPing[0], h.linkPing[1]
	}
	h.mu.Lock()
	if h.wsStopping {
		h.mu.Unlock()
		cancel(errors.New("hub stopping"))
		_ = conn.Close(websocket.StatusGoingAway, "hub stopping")
		return
	}
	if h.links == nil {
		h.links = map[*hubLink]struct{}{}
	}
	h.links[l] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.links, l)
		h.mu.Unlock()
	}()
	if old := m.setLink(l); old != nil {
		old.close(link.CloseReplaced, "a newer link of this machine connected")
	}
	h.logf("[%s] link up: jarvis-link %s via %s from %s", m.cfg.ID, l.version, l.address, maskedSource(r))
	_ = l.send(h.linkEndpoints())
	go l.keepAlive()
	// The read loop is not bound to l.ctx: a link closed with a reason (replaced, revoked) still sends its close frame.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			cancel(fmt.Errorf("link closed: %w", err))
			break
		}
		var msg link.Message
		if json.Unmarshal(data, &msg) == nil && msg.Type == "open_failed" {
			l.deliver(msg.ID, linkStream{err: fmt.Errorf("jarvis-link: %s", clip(msg.Error, 300))})
		}
	}
	cause := context.Cause(ctx)
	m.dropLink(l, cause)
	h.logf("[%s] link down: %v", m.cfg.ID, cause)
}

func (h *Hub) serveLinkStream(w http.ResponseWriter, r *http.Request) {
	m, p, ok := h.linkCaller(w, r)
	if !ok {
		return
	}
	l, id := m.currentLink(), r.URL.Query().Get("id")
	if l == nil || l.tokenID != p.ID || !l.wants(id) {
		writeErr(w, http.StatusNotFound, errors.New("no such stream"))
		return
	}
	if !h.enterWS() {
		http.Error(w, "hub stopping", http.StatusServiceUnavailable)
		return
	}
	defer h.wsHandlers.Done()
	// Compressed like the ssh connections (Compression=yes, machine.go): node traffic is JSON.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true,
		CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	done := make(chan struct{})
	if !l.deliver(id, linkStream{conn: conn, done: done}) {
		_ = conn.Close(websocket.StatusNormalClosure, "stream no longer wanted")
		return
	}
	select {
	case <-done:
	case <-l.ctx.Done():
	}
}

func (l *hubLink) wants(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.pending[id]
	return ok
}

// deliver hands a stream (or why there is none) to the connect waiting for it; false when none is.
func (l *hubLink) deliver(id string, s linkStream) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.pending[id]
	if !ok {
		return false
	}
	delete(l.pending, id)
	ch <- s // buffered: one delivery per id
	return true
}

// open asks jarvis-link for a stream to the resident node and waits for it.
func (l *hubLink) open(ctx context.Context) (*websocket.Conn, chan struct{}, error) {
	id := randomHex(12)
	ch := make(chan linkStream, 1)
	l.mu.Lock()
	l.pending[id] = ch
	l.mu.Unlock()
	var err error
	if err = l.send(link.Message{Type: "open", ID: id}); err == nil {
		timer := time.NewTimer(linkOpenTimeout)
		defer timer.Stop()
		select {
		case s := <-ch:
			if s.err != nil {
				return nil, nil, s.err
			}
			return s.conn, s.done, nil
		case <-timer.C:
			err = errors.New("jarvis-link did not open the stream in time")
		case <-l.ctx.Done():
			err = context.Cause(l.ctx)
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	l.mu.Lock()
	delete(l.pending, id)
	l.mu.Unlock()
	select { // delivered just now: not wanted any more
	case s := <-ch:
		if s.conn != nil {
			_ = s.conn.Close(websocket.StatusNormalClosure, "stream no longer wanted")
			close(s.done)
		}
	default:
	}
	return nil, nil, err
}

// send writes one message. Not under l.ctx: coder/websocket closes the connection when a write's context ends, and
// a link being closed with a reason (replaced, revoked) must still get its close frame out.
func (l *hubLink) send(msg link.Message) error {
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	b, _ := json.Marshal(msg)
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	return l.conn.Write(ctx, websocket.MessageText, b)
}

// keepAlive pings jarvis-link; a link without pongs is dead (the network under the laptop went away).
func (l *hubLink) keepAlive() {
	t := time.NewTicker(l.every)
	defer t.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), l.timeout) // not l.ctx: see send
			start := time.Now()
			err := l.conn.Ping(ctx)
			cancel()
			if err != nil && l.ctx.Err() == nil {
				l.cancel(fmt.Errorf("no pong within %s", l.timeout))
				_ = l.conn.CloseNow()
				return
			}
			if err == nil {
				l.rtt.Store(int64(time.Since(start)))
			}
		}
	}
}

// close ends the link: its streams at once, the link itself with a close frame saying why.
func (l *hubLink) close(code websocket.StatusCode, reason string) {
	l.cancel(errors.New(reason))
	go func() { _ = l.conn.Close(code, reason) }()
}

// linkEndpoints is where the hub can be found, and what it is called and how many hubs its mesh has (the Mac menu
// bar shows both, docs/link.md).
func (h *Hub) linkEndpoints() link.Message {
	msg := link.Message{Type: "endpoints", HubName: h.hubName(), MeshHubs: h.meshHubCount()}
	for _, e := range h.endpointList() {
		if e.Scheme == "https" {
			msg.Endpoints = append(msg.Endpoints, link.Endpoint{URL: e.URL, Kind: e.Kind})
		}
	}
	return msg
}

// sendLinkEndpoints tells every link where the hub can be found now and what its mesh is: its addresses or the mesh
// changed.
func (h *Hub) sendLinkEndpoints() {
	msg := h.linkEndpoints()
	h.mu.Lock()
	links := make([]*hubLink, 0, len(h.links))
	for l := range h.links {
		links = append(links, l)
	}
	h.mu.Unlock()
	for _, l := range links {
		go func() { _ = l.send(msg) }()
	}
}

// closeLinks closes the links that came with a revoked credential.
func (h *Hub) closeLinks(tokenID string) int {
	h.mu.Lock()
	var gone []*hubLink
	for l := range h.links {
		if l.tokenID == tokenID {
			gone = append(gone, l)
		}
	}
	h.mu.Unlock()
	for _, l := range gone {
		l.close(link.CloseRevoked, "credential revoked")
	}
	return len(gone)
}

// stopLinks is part of the shutdown: every link gets a 1001 so jarvis-link comes back to the next hub quickly.
func (h *Hub) stopLinks() []*hubLink {
	h.mu.Lock()
	links := make([]*hubLink, 0, len(h.links))
	for l := range h.links {
		links = append(links, l)
	}
	h.mu.Unlock()
	for _, l := range links {
		l.cancel(errors.New("hub stopping"))
	}
	return links
}

// currentLink is the machine's live link; nil while there is none, or while the one there is going away.
func (m *Machine) currentLink() *hubLink {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.link == nil || m.link.ctx.Err() != nil {
		return nil
	}
	return m.link
}

// setLink makes l the machine's link and has run connect through it now; it returns the link it replaced.
func (m *Machine) setLink(l *hubLink) *hubLink {
	m.mu.Lock()
	old := m.link
	m.link, m.linkDown = l, nil
	m.mu.Unlock()
	for _, ch := range []chan struct{}{m.linkUp, m.kick} {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	m.hub.wakeOwnerOf(m) // the machine whose path this entry is (mesh_paths.go)
	m.hub.broadcastMachines()
	return old
}

// dropLink forgets l if it is still the machine's link. A connection on it ends by itself (its stream goes with
// l.ctx); a machine that was not connected shows why the link went away.
func (m *Machine) dropLink(l *hubLink, cause error) {
	m.mu.Lock()
	if m.link != l {
		m.mu.Unlock()
		return
	}
	m.link, m.linkDown = nil, cause
	if !m.online && cause != nil {
		m.err = cause.Error()
	}
	m.mu.Unlock()
	m.hub.broadcastMachines()
}

// waitLink blocks until the machine has a link; false when ctx ends first.
func (m *Machine) waitLink(ctx context.Context) bool {
	for m.currentLink() == nil {
		select {
		case <-ctx.Done():
			return false
		case <-m.linkUp:
		}
	}
	return true
}

// dialLink opens a stream on the machine's link: the node's bytes, as ssh's stdout and stdin are for other machines.
func (m *Machine) dialLink(ctx context.Context) (*nodePipe, error) {
	m.mu.Lock()
	l, down := m.link, m.linkDown
	m.mu.Unlock()
	switch {
	case l == nil && down != nil:
		return nil, down
	case l == nil:
		return nil, errors.New("waiting for jarvis-link")
	case l.ctx.Err() != nil:
		return nil, context.Cause(l.ctx)
	}
	conn, done, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancelCause(l.ctx)
	nc := websocket.NetConn(sctx, conn, websocket.MessageBinary)
	go func() { // a stream whose TCP connection died quietly, while its link still answers
		t := time.NewTicker(l.every)
		defer t.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(sctx, l.timeout)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil && sctx.Err() == nil {
					cancel(fmt.Errorf("stream: no pong within %s", l.timeout))
					return
				}
			}
		}
	}()
	var once sync.Once
	return &nodePipe{r: nc, w: nc,
		lastErr: func() string {
			if c := context.Cause(sctx); c != nil && !errors.Is(c, context.Canceled) {
				return c.Error()
			}
			return ""
		},
		close: func() {
			once.Do(func() {
				cancel(nil)
				_ = nc.Close()
				close(done)
			})
		}}, nil
}

// tellLink says on the machine's link whether the hub got through to the resident node: jarvis-link shows it.
func (m *Machine) tellLink(online bool, err string) {
	if m.cfg.Kind != kindLink {
		return
	}
	if l := m.currentLink(); l != nil {
		go func() { _ = l.send(link.Message{Type: "node", Online: online, Error: clip(err, 300)}) }()
	}
}

// rttMS is the link's last ping round trip in ms (0 before the first).
func (l *hubLink) rttMS() int {
	if d := l.rtt.Load(); d > 0 {
		return max(1, int(time.Duration(d).Milliseconds()))
	}
	return 0
}

func (l *hubLink) info() *proto.MachineLink {
	return &proto.MachineLink{Version: l.version, Address: l.address, Since: l.since}
}
