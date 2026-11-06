package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

// relayingExt is the manager's extensions faked (fakeExt) that also serves what the hub relays to it, as the manager
// does: on extensions.accept it dials the relay stream and serves HTTP on it with serve.
type relayingExt struct {
	*fakeExt
	serve http.Handler
	dial  func(ctx context.Context, query string) (*websocket.Conn, error)
}

func (x *relayingExt) Request(ctx context.Context, method string, params json.RawMessage) (any, bool, error) {
	switch method {
	case extwire.MethodAccept:
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &a); err != nil || a.ID == "" {
			return nil, true, errors.New("extensions.accept needs an id")
		}
		conn, err := x.dial(ctx, url.Values{"id": {a.ID}}.Encode())
		if err != nil {
			return nil, true, err
		}
		// As the manager serves the relay (extmgr Accept): over a pipe, whose deadlines the server may set.
		nc := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)
		mine, theirs := net.Pipe()
		go func() {
			defer nc.Close()
			defer theirs.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(theirs, nc); done <- struct{}{} }()
			go func() { _, _ = io.Copy(nc, theirs); done <- struct{}{} }()
			<-done
		}()
		go func() { _ = (&http.Server{Handler: x.serve}).Serve(newOneConnListener(mine)) }()
		return map[string]any{}, true, nil
	case extwire.MethodAuthorize:
		// The manager judges /api/v1/extensions/<id>/… by its own route (extmgr authorize).
		var a extwire.Authorize
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, true, err
		}
		if a.Pattern == "/api/v1/extensions/" {
			a.Pattern = "/api/v1/extensions/{id}/"
		}
		raw, _ := json.Marshal(a)
		return x.fakeExt.Request(ctx, method, raw)
	}
	return x.fakeExt.Request(ctx, method, params)
}

// oneConnListener gives one connection, then waits until that connection is closed.
type oneConnListener struct {
	mu   sync.Mutex
	c    net.Conn
	once sync.Once
	done chan struct{}
}

func newOneConnListener(c net.Conn) *oneConnListener {
	l := &oneConnListener{done: make(chan struct{})}
	l.c = &closingConn{Conn: c, closed: func() { _ = l.Close() }}
	return l
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	c := l.c
	l.c = nil
	l.mu.Unlock()
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *oneConnListener) Addr() net.Addr { return &net.UnixAddr{Name: "relay", Net: "unix"} }

type closingConn struct {
	net.Conn
	closed func()
}

func (c *closingConn) Close() error { err := c.Conn.Close(); c.closed(); return err }

// relayedManager is what a test's fake manager saw relayed: the X-Persona-* headers of a request (its answer) and of
// a WebSocket (its first message, then it echoes); each WebSocket's end on its side is sent on ended, with its device.
type relayedManager struct {
	ended chan string
}

func personaHeaders(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-persona-") {
			out[k] = strings.Join(v, ",")
		}
	}
	return out
}

func (m *relayedManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/ws") {
		_ = json.NewEncoder(w).Encode(personaHeaders(r))
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	device := r.Header.Get(extwire.HeaderDevice)
	defer func() { m.ended <- device }()
	b, _ := json.Marshal(personaHeaders(r))
	if c.Write(r.Context(), websocket.MessageText, b) != nil {
		return
	}
	for {
		typ, b, err := c.Read(r.Context())
		if err != nil {
			return
		}
		if c.Write(r.Context(), typ, b) != nil {
			return
		}
	}
}

// relayTestHub is a hub with device tokens and pairing codes in memory, its manager connected over the bus and serving the relayed
// routes with a relayedManager; the apps reach it at the server returned.
func relayTestHub(t *testing.T) (*Hub, *relayedManager, *fakeExt, *httptest.Server) {
	t.Helper()
	h := newTestHub(t)
	h.store = &pairStore{tokenStore: tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}},
		pairs: map[string]db.PairToken{}}
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	busMux := http.NewServeMux()
	h.busRoutes(busMux)
	busSrv := httptest.NewServer(busMux)
	t.Cleanup(busSrv.Close)
	m := &relayedManager{ended: make(chan string, 8)}
	var token string
	x := &relayingExt{fakeExt: fakeExtOf(h), serve: m, dial: func(ctx context.Context, query string) (*websocket.Conn, error) {
		return bus.DialAt(ctx, busSrv.URL, token, busSrv.Client(), devicewire.RelayPath, query)
	}}
	a := dialAssistant(t, h, busSrv.URL, false, func(o *assistant.BusOptions) { o.Extensions = x })
	token = a.token
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)
	return h, m, x.fakeExt, srv
}

// relayDevice makes a device token of kind ("phone": full, "watch": answer-only) with by's credential.
func relayDevice(t *testing.T, srv *httptest.Server, by, name, kind string) (id, token string) {
	t.Helper()
	code, out := call(t, srv.Config.Handler, "POST", "/api/v1/auth/tokens", by, `{"name":"`+name+`","kind":"`+kind+`"}`)
	if code != 200 {
		t.Fatalf("token for %s: %d %v", name, code, out)
	}
	return out["id"].(string), out["token"].(string)
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// What the hub relays to the manager for an extension's own HTTP says who asked, as the hub's auth found it, on the
// request and the WebSocket alike, whatever the client sent under X-Persona-*.
func TestRelayedCallerHeaders(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	ext := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	forged := http.Header{"X-Persona-Device": {"someone-else"}, "X-Persona-Role": {"hub"}, "X-Persona-Owner": {"1"},
		"X-Persona-Admin": {"1"}, "X-Persona-Scopes": {"kb.read"}, "X-Persona-Forged": {"1"}}
	get := func(token, lang string) map[string]string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/extensions/callee/echo", nil)
		for k, v := range forged {
			req.Header[k] = v
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]string{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, out)
		}
		return out
	}
	for name, c := range map[string]struct {
		got  map[string]string
		want map[string]string
	}{
		"a full device": {get(phone, "en-US"), map[string]string{"X-Persona-Device": phoneID, "X-Persona-Device-Name": "iPhone",
			"X-Persona-Role": roleFull, "X-Persona-Lang": "en", "X-Persona-Remote": "127.0.0.1"}},
		"another extension's token": {get(ext, "ja"), map[string]string{"X-Persona-Device": "extension:caller",
			"X-Persona-Device-Name": "Caller", "X-Persona-Role": roleExtension, "X-Persona-Extension": "caller",
			"X-Persona-Scopes": extension.ScopeCallPrefix + "callee", "X-Persona-Lang": "ja", "X-Persona-Remote": "127.0.0.1"}},
		// No device: the master token, a full caller (the manager passes Device / Role / Lang on to the extension as is).
		"the master token": {get(master, "zh-CN"), map[string]string{"X-Persona-Role": roleFull, "X-Persona-Lang": "zh-Hans",
			"X-Persona-Remote": "127.0.0.1", "X-Persona-Owner": "1"}},
	} {
		if len(c.got) != len(c.want) {
			t.Fatalf("%s: %v, want %v", name, c.got, c.want)
		}
		for k, v := range c.want {
			if c.got[k] != v {
				t.Fatalf("%s: %s = %q, want %q (%v)", name, k, c.got[k], v, c.got)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hd := http.Header{"Authorization": {"Bearer " + phone}, "Accept-Language": {"ja"}}
	for k, v := range forged {
		hd[k] = v
	}
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"), &websocket.DialOptions{HTTPHeader: hd})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	_ = json.Unmarshal(b, &got)
	if got["X-Persona-Device"] != phoneID || got["X-Persona-Role"] != roleFull || got["X-Persona-Lang"] != "ja" ||
		got["X-Persona-Owner"] != "" || got["X-Persona-Admin"] != "" || got["X-Persona-Forged"] != "" {
		t.Fatalf("WebSocket: %v", got)
	}
}

// Revoking a device ends the WebSockets it has open through the manager to the extensions, both sides, at once; another
// device's and the master token's stay.
func TestRevokingEndsRelayedSockets(t *testing.T) {
	h, m, _, srv := relayTestHub(t)
	master := h.cfg.Token
	phoneID, phone := relayDevice(t, srv, master, "iPhone", "phone")
	_, mac := relayDevice(t, srv, master, "Mac", "mac")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(token string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
			&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Read(ctx); err != nil { // who asked
			t.Fatal(err)
		}
		return c
	}
	roundTrip := func(c *websocket.Conn) error {
		if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
			return err
		}
		_, b, err := c.Read(ctx)
		if err == nil && string(b) != `{"type":"ping"}` {
			t.Fatalf("echo: %q", b)
		}
		return err
	}
	phoneWS, macWS, masterWS := open(phone), open(mac), open(master)
	defer phoneWS.CloseNow()
	defer macWS.CloseNow()
	defer masterWS.CloseNow()
	for _, c := range []*websocket.Conn{phoneWS, macWS, masterWS} {
		if err := roundTrip(c); err != nil {
			t.Fatal(err)
		}
	}

	if code, out := call(t, srv.Config.Handler, "DELETE", "/api/v1/auth/tokens/"+phoneID, master, ""); code != 200 {
		t.Fatalf("revoke: %d %v", code, out)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	if _, _, err := phoneWS.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the revoked phone's WebSocket is still open: %v", err)
	}
	select {
	case device := <-m.ended:
		if device != phoneID {
			t.Fatalf("ended on the manager's side: %q, want the phone's", device)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side of the revoked phone's WebSocket is still open")
	}
	for name, c := range map[string]*websocket.Conn{"mac": macWS, "master": masterWS} {
		if err := roundTrip(c); err != nil {
			t.Fatalf("%s's WebSocket after the phone's revocation: %v", name, err)
		}
	}
	h.ext.calls.mu.Lock()
	kept := len(h.ext.calls.byID[phoneID])
	h.ext.calls.mu.Unlock()
	if kept != 0 {
		t.Fatalf("still kept for the revoked phone: %d", kept)
	}
}

// A WebSocket opened with a pairing code through the manager ends when the code does (here its expiry; revoking the
// device that redeemed it expires it too), as the code's own /api/v1/ws does: it is not a device revoked() is told of.
func TestRelayedPairingCallEndsWithTheCode(t *testing.T) {
	h, m, _, srv := relayTestHub(t)
	tok := "jp_" + strings.Repeat("ab", 32)
	device := "pair_" + hashToken(tok)
	if err := h.store.CreatePairToken(context.Background(), hashToken(tok), time.Now().Add(1500*time.Millisecond), ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/extensions/callee/ws"),
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + tok}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	_ = json.Unmarshal(b, &got)
	if got["X-Persona-Device"] != device || got["X-Persona-Role"] != roleFull || got["X-Persona-Paired"] != "1" {
		t.Fatalf("who asked: %v", got)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 6*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil || readCtx.Err() != nil {
		t.Fatalf("the WebSocket outlived its pairing code: %v", err)
	}
	select {
	case ended := <-m.ended:
		if ended != device {
			t.Fatalf("ended on the manager's side: %q, want the pairing code's", ended)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the manager's side outlived the pairing code")
	}
}

// A client cannot drop the hub's headers by naming them in Connection (a hop-by-hop header the proxy removes): a
// pairing code stays paired, an extension's token stays the extension's.
func TestRelayedCallerHeadersSurviveConnection(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	tok := "jp_" + strings.Repeat("cd", 32)
	if err := h.store.CreatePairToken(context.Background(), hashToken(tok), time.Now().Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	ext := fx.issue("caller", "Caller", []string{extension.ScopeCallPrefix + "callee"})
	get := func(token string) map[string]string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/extensions/callee/echo", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Connection", "X-Persona-Paired, X-Persona-Role, X-Persona-Device, X-Persona-Extension")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out := map[string]string{}
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, out)
		}
		return out
	}
	if got := get(tok); got["X-Persona-Paired"] != "1" || got["X-Persona-Role"] != roleFull || got["X-Persona-Device"] != "pair_"+hashToken(tok) {
		t.Fatalf("pairing code: %v", got)
	}
	if got := get(ext); got["X-Persona-Role"] != roleExtension || got["X-Persona-Extension"] != "caller" {
		t.Fatalf("extension token: %v", got)
	}
}

// A request that passed the auth just before its device was revoked ends as it starts; the master token's are not
// kept, a device's are until done.
func TestRelayedCallRevokedBeforeTracked(t *testing.T) {
	h := newTestHub(t)
	as := func(id string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil)
		return r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{ID: id, Role: roleFull}))
	}
	h.revoked("dev_gone")
	r, done := h.trackRelayed(as("dev_gone"))
	if r.Context().Err() == nil {
		t.Fatal("a revoked device's call goes on")
	}
	done()
	r, done = h.trackRelayed(httptest.NewRequest("GET", "/api/v1/extensions/x/ws", nil))
	done()
	if r.Context().Err() != nil {
		t.Fatal("the master token's request was ended")
	}
	r, done = h.trackRelayed(as("dev_live"))
	if n := h.dropRelayed("dev_live"); n != 1 || r.Context().Err() == nil {
		t.Fatalf("dropped %d, err %v", n, r.Context().Err())
	}
	done()
	h.ext.calls.mu.Lock()
	left := len(h.ext.calls.byID)
	h.ext.calls.mu.Unlock()
	if left != 0 {
		t.Fatalf("left kept: %d", left)
	}
}
