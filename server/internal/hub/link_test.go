package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/rpc"
)

// newLinkHub is a hub with a link machine "laptop" and an ssh machine "box", served over TLS as on the public port.
func newLinkHub(t *testing.T) (*Hub, *Machine, *httptest.Server) {
	t.Helper()
	h := newTestHub(t)
	h.store = newPairStore()
	for _, mc := range []MachineConfig{{ID: "laptop", Name: "笔记本", Kind: "link"}, {ID: "box", Name: "台式机", Kind: "ssh"}} {
		m := newMachine(h, mc)
		h.machines = append(h.machines, m)
		h.byID[mc.ID] = m
	}
	srv := httptest.NewTLSServer(h.routes())
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	h.public.mu.Lock()
	h.public.port, h.public.fp = port, spkiPin(srv.Certificate())
	h.public.endpoints = []Endpoint{{URL: srv.URL, Kind: "lan", Port: port, Scheme: "https"}}
	h.public.mu.Unlock()
	h.cfg.PublicURL = "http://127.0.0.1:" + strconv.Itoa(port)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.byID["laptop"].run(ctx)
	return h, h.byID["laptop"], srv
}

func linkToken(t *testing.T, h *Hub, machine string) (token, id string) {
	t.Helper()
	token, id = "jd_"+randomHex(24), "dev_"+randomHex(6)
	dt := db.DeviceToken{ID: id, Name: "link", Kind: "link", Role: roleLink, MachineID: machine}
	if err := h.store.CreateDeviceToken(context.Background(), dt, hashToken(token)); err != nil {
		t.Fatal(err)
	}
	return token, id
}

// fakeResidentNode listens like `jarvisd node --attach` and answers every call of every connection with its number.
func fakeResidentNode(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "jl") // unix socket paths are short on macOS
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "attach.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var n atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			id := n.Add(1)
			go func() {
				defer c.Close()
				p := rpc.New("n", c)
				p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
					return map[string]any{"pid": id, "hostname": "laptop"}, nil
				}
				_ = p.Serve(context.Background(), c)
			}()
		}
	}()
	return path
}

// runLink runs jarvis-link (internal/link) with cfg; stop ends it.
func runLink(t *testing.T, cfg link.Config, target string) (statuses chan link.Status, path string, stop func()) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "link.json")
	if err := link.Update(path, func(c *link.Config) error { *c = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	statuses = make(chan link.Status, 256)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = link.Run(ctx, link.Options{Path: path, Target: target, Version: "test-build",
			Status: func(s link.Status) {
				select {
				case statuses <- s:
				default:
				}
			}})
	}()
	var once atomic.Bool
	stop = func() {
		if once.CompareAndSwap(false, true) {
			cancel()
			<-done
		}
	}
	t.Cleanup(stop)
	return statuses, path, stop
}

func linkConfig(h *Hub, srv *httptest.Server, token string) link.Config {
	return link.Config{Hub: "hub", Machine: "laptop", FP: spkiPin(srv.Certificate()), Token: token,
		Endpoints: []link.Endpoint{{URL: srv.URL, Kind: "lan"}}}
}

func eventually(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %s: %s", within, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitStatus(t *testing.T, ch chan link.Status, what string, ok func(link.Status) bool) link.Status {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case s := <-ch:
			if ok(s) {
				return s
			}
		case <-timeout:
			t.Fatalf("jarvis-link never reported %s", what)
		}
	}
}

func pid(t *testing.T, m *Machine) int {
	t.Helper()
	var out struct {
		PID int `json:"pid"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.call(ctx, "ping", map[string]any{}, &out); err != nil {
		t.Fatalf("call over the link: %v", err)
	}
	return out.PID
}

func TestLinkMachineComesOnlineThroughJarvisLink(t *testing.T) {
	h, m, srv := newLinkHub(t)
	token, _ := linkToken(t, h, "laptop")
	target := fakeResidentNode(t)
	cfg := linkConfig(h, srv, token)
	cfg.Endpoints = append(cfg.Endpoints, link.Endpoint{URL: "https://hub.example.com:18662", Kind: "name", Manual: true})
	statuses, path, stop := runLink(t, cfg, target)

	eventually(t, 10*time.Second, "machine online", func() bool { return m.snapshot().Online })
	first := pid(t, m)
	st := waitStatus(t, statuses, "the node online", func(s link.Status) bool { return s.State == "connected" && s.Node == "online" })
	if st.Address != srv.URL || st.Kind != "lan" || st.Since == nil {
		t.Fatalf("status %+v", st)
	}
	if l := m.snapshot().Link; l == nil || l.Version != "test-build" || l.Address != srv.URL {
		t.Fatalf("machine list shows link %+v", l)
	}
	// The hub's endpoints replaced the ones from pairing; the one added on the laptop stayed.
	eventually(t, 5*time.Second, "endpoints saved", func() bool {
		c, err := link.Load(path)
		return err == nil && len(c.Endpoints) == 2 && c.Endpoints[0].Manual && c.Endpoints[1].URL == srv.URL
	})

	// The link going away takes the machine offline right then, not at the next ssh-style retry.
	stop()
	eventually(t, 3*time.Second, "machine offline", func() bool { return !m.snapshot().Online && m.snapshot().Link == nil })
	if err := m.call(context.Background(), "ping", nil, nil); !errors.Is(err, errOffline) {
		t.Fatalf("call while offline: %v", err)
	}

	statuses, _, _ = runLink(t, cfg, target)
	eventually(t, 10*time.Second, "machine online again", func() bool { return m.snapshot().Online })
	if again := pid(t, m); again == first {
		t.Fatal("still the old node connection")
	}
	waitStatus(t, statuses, "connected again", func(s link.Status) bool { return s.State == "connected" })
}

func TestLinkWithoutResidentNode(t *testing.T) {
	h, m, srv := newLinkHub(t)
	token, _ := linkToken(t, h, "laptop")
	statuses, _, _ := runLink(t, linkConfig(h, srv, token), "/tmp/jarvis-link-no-such.sock")
	st := waitStatus(t, statuses, "the node missing", func(s link.Status) bool { return s.State == "connected" && s.Node == "offline" })
	if !strings.Contains(st.NodeError, "常驻 node") {
		t.Fatalf("node error %q", st.NodeError)
	}
	eventually(t, 5*time.Second, "the machine says why", func() bool { return strings.Contains(m.snapshot().Error, "常驻 node") })
	if m.snapshot().Online || m.snapshot().Link == nil {
		t.Fatal("the link is up but the node is not")
	}
}

func dialLink(t *testing.T, srv *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "wss"+strings.TrimPrefix(srv.URL, "https")+link.Path,
		&websocket.DialOptions{HTTPClient: srv.Client(), HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
}

func hello(t *testing.T, c *websocket.Conn, machine string) {
	t.Helper()
	b, _ := json.Marshal(link.Message{Type: "hello", Machine: machine, Version: "manual"})
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// closedWith reads until the hub closes the link and returns its close code.
func closedWith(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestLinkReplacedAndRevoked(t *testing.T) {
	h, m, srv := newLinkHub(t)
	token, id := linkToken(t, h, "laptop")
	statuses, _, _ := runLink(t, linkConfig(h, srv, token), fakeResidentNode(t))
	eventually(t, 10*time.Second, "machine online", func() bool { return m.snapshot().Online })

	// A second jarvis-link with the same credential takes over; the first is told so.
	c, _, err := dialLink(t, srv, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	hello(t, c, "laptop")
	waitStatus(t, statuses, "being replaced", func(s link.Status) bool {
		return s.State == "waiting" && strings.Contains(s.Error, "又连了一条")
	})
	eventually(t, 3*time.Second, "the new link in place", func() bool {
		l := m.currentLink()
		return l != nil && l.version == "manual"
	})

	h.revoked(id)
	if code := closedWith(t, c); code != link.CloseRevoked {
		t.Fatalf("revoked link closed with %d", code)
	}
	eventually(t, 3*time.Second, "no link", func() bool { return m.currentLink() == nil })
}

func TestLinkRefusesWhatItShouldNot(t *testing.T) {
	h, _, srv := newLinkHub(t)
	token, _ := linkToken(t, h, "laptop")
	phone := "jd_" + randomHex(24)
	_ = h.store.CreateDeviceToken(context.Background(), db.DeviceToken{ID: "dev_phone", Name: "iPhone", Kind: "phone", Role: roleFull}, hashToken(phone))
	boxToken, _ := linkToken(t, h, "box")

	for _, tc := range []struct {
		name, token string
		status      int
	}{
		{"a phone's credential", phone, http.StatusForbidden},
		{"a link credential of a machine that is not kind link", boxToken, http.StatusNotFound},
		{"no credential", "", http.StatusUnauthorized},
	} {
		if _, resp, err := dialLink(t, srv, tc.token); err == nil || resp == nil || resp.StatusCode != tc.status {
			t.Errorf("%s: %v %v, want %d", tc.name, err, resp, tc.status)
		}
	}
	// A link credential opens nothing else.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := srv.Client().Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("machines with a link credential: %v %v", err, resp)
	}
	// Plaintext: refused from the LAN as well, only loopback may skip TLS.
	w := securityRequest(h.routes(), "GET", link.Path, token, "", "[fd00::20]:5000", false)
	requireCode(t, w, http.StatusForbidden, "tls_required")
	// A hello for another machine.
	c, _, err := dialLink(t, srv, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	hello(t, c, "box")
	if code := closedWith(t, c); code != link.CloseRefused {
		t.Fatalf("hello for another machine closed with %d", code)
	}
}

func TestLinkHeartbeatDropsADeadLink(t *testing.T) {
	h, m, srv := newLinkHub(t)
	h.linkPing = [2]time.Duration{50 * time.Millisecond, 50 * time.Millisecond}
	token, _ := linkToken(t, h, "laptop")
	c, _, err := dialLink(t, srv, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	hello(t, c, "laptop") // and never read again: pongs are only sent while reading
	eventually(t, 3*time.Second, "link up", func() bool { return m.currentLink() != nil })
	eventually(t, 3*time.Second, "dead link dropped", func() bool { return m.currentLink() == nil })
	// The link is gone once its context is cancelled; the machine says why a moment later (dropLink).
	eventually(t, 3*time.Second, "the machine says why", func() bool { return strings.Contains(m.snapshot().Error, "no pong") })
}

func TestLinkPairing(t *testing.T) {
	h, m, _ := newLinkHub(t)
	code := func(body string) *httptest.ResponseRecorder {
		return securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, body, "127.0.0.1:1", false)
	}
	requireCode(t, code(`{"machine":"box"}`), http.StatusBadRequest, "")
	requireCode(t, code(`{"machine":"nope"}`), http.StatusBadRequest, "")
	w := code(`{"machine":"laptop"}`)
	requireCode(t, w, http.StatusOK, "")
	var p pairCode
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Machine != "laptop" || !strings.HasPrefix(p.PairURL, "jarvis://link?") || !strings.Contains(p.PairURL, "machine=laptop") {
		t.Fatalf("pair code %+v", p)
	}
	// What a code made for the laptop can do: become the laptop's link credential, nothing else.
	requireCode(t, securityRequest(h.routes(), "GET", "/api/v1/machines", p.Token, "", "127.0.0.1:1", false), http.StatusForbidden, "")
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/tokens", p.Token, `{"kind":"link","machine":"box"}`, "127.0.0.1:1", false),
		http.StatusForbidden, "")

	path := filepath.Join(t.TempDir(), "link.json")
	c, err := link.Pair(context.Background(), p.PairURL, "", "", path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := link.Load(path)
	if err != nil || saved.Machine != "laptop" || saved.Token == "" || saved.FP != h.public.fp || len(saved.Endpoints) == 0 || c.Token != saved.Token {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	who, ok := h.authenticate(context.Background(), saved.Token)
	if !ok || who.Role != roleLink || who.Machine != "laptop" {
		t.Fatalf("redeemed credential %+v", who)
	}
	if _, err := link.Pair(context.Background(), p.PairURL, "", "", path); err == nil {
		t.Fatal("a pairing code works twice")
	}
	// The credential it wrote opens the link.
	statuses, _, _ := runLink(t, *saved, fakeResidentNode(t))
	waitStatus(t, statuses, "connected", func(s link.Status) bool { return s.State == "connected" })
	eventually(t, 10*time.Second, "machine online", func() bool { return m.snapshot().Online })

	// The apps' pairing code can make one too, for a link machine named in the request.
	w = code("")
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if strings.HasPrefix(p.PairURL, "jarvis://link?") {
		t.Fatal("a code for the apps")
	}
	if _, err := link.Pair(context.Background(), p.PairURL, "box", "", filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Fatal("a link credential for an ssh machine")
	}
	if _, err := link.Pair(context.Background(), p.PairURL, "laptop", "", filepath.Join(t.TempDir(), "y.json")); err != nil {
		t.Fatal(err)
	}

	// No TLS, no jarvis-link.
	h.public.mu.Lock()
	h.public.port = 0
	h.public.mu.Unlock()
	requireCode(t, code(`{"machine":"laptop"}`), http.StatusConflict, "")
}

func TestLinkTellsWhatTheHubIsCalled(t *testing.T) {
	h, _, srv := newLinkHub(t)
	h.cfg.Name = "家里"
	token, _ := linkToken(t, h, "laptop")
	statuses, path, _ := runLink(t, linkConfig(h, srv, token), fakeResidentNode(t))
	st := waitStatus(t, statuses, "the hub's name", func(s link.Status) bool { return s.State == "connected" && s.HubName != "" })
	if st.HubName != "家里" || st.MeshHubs != 1 {
		t.Fatalf("status %+v", st)
	}
	eventually(t, 5*time.Second, "the name kept in link.json", func() bool {
		c, err := link.Load(path)
		return err == nil && c.HubName == "家里"
	})
	if m := h.linkEndpoints(); m.Type != "endpoints" || m.HubName != "家里" || m.MeshHubs != 1 || len(m.Endpoints) != 1 {
		t.Fatalf("endpoints message %+v", m)
	}
}

func TestHubName(t *testing.T) {
	h := newTestHub(t)
	h.public.id = "3f2a9c1b-0000-4000-8000-000000000000"
	host := ""
	old := hostname
	hostname = func() (string, error) { return host, nil }
	t.Cleanup(func() { hostname = old })
	for _, tc := range []struct{ name, host, local, want string }{
		{"", "nas.local", "", "nas"},
		{"", "MacBook-Pro", "", "MacBook-Pro"},
		{"", "a1b2c3d4e5f6", "", "hub-3f2a9c"}, // a Docker container's id
		{"", strings.Repeat("ab", 32), "", "hub-3f2a9c"},
		{"", "", "", "hub-3f2a9c"},
		{"", "localhost", "", "hub-3f2a9c"},
		{"", "nas", "0", "hub-3f2a9c"}, // the Docker image
		{"  家里\n 书房 ", "nas", "0", "家里 书房"},
		{strings.Repeat("名", 80), "nas", "", strings.Repeat("名", 64) + "…"},
	} {
		h.cfg.Name, host = tc.name, tc.host
		t.Setenv("JARVIS_LOCAL_NODE", tc.local)
		if got := h.hubName(); got != tc.want {
			t.Errorf("name %q, host %q, JARVIS_LOCAL_NODE=%q: %q, want %q", tc.name, tc.host, tc.local, got, tc.want)
		}
	}
	if h.meshHubCount() != 1 {
		t.Fatal("a hub alone is a mesh of one")
	}
}
