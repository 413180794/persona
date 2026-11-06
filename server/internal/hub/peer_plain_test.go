package hub

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Two hubs without TLS pair and link over plain HTTP when each lists the other's address (#4514), and what apps
// are shown of the group never carries a plaintext address.
func TestMeshPlaintextLink(t *testing.T) {
	allow := []string{"127.0.0.1"}
	a := startTestHubOpts(t, hubA, "家里", newMeshStore(), "", nil, nil, testHubOpts{plain: true, plainAllow: allow})
	b := startTestHubOpts(t, hubB, "Office", newMeshStore(), "", nil, nil, testHubOpts{plain: true, plainAllow: allow})
	pairHubs(t, a, b)
	l := a.h.peerLinkTo(hubB)
	if l == nil {
		t.Fatal("no link")
	}
	a.h.mesh.mu.Lock()
	row := a.h.mesh.peers[hubB]
	a.h.mesh.mu.Unlock()
	if row.FP != "" || len(row.Endpoints) == 0 || !isPlainEndpoint(row.Endpoints[0]) {
		t.Fatalf("pairing kept %+v", row)
	}
	for _, mh := range a.h.meshHubs() {
		if mh.Self { // a hub without TLS shows apps its own plaintext addresses, as before
			continue
		}
		for _, e := range mh.Endpoints {
			if !strings.HasPrefix(e, "https://") {
				t.Fatalf("apps are shown %q for %s", e, mh.Name)
			}
		}
	}
}

// Without the other hub's address in mesh.plaintext.allow, or with plaintext off, a hub that has no TLS cannot be
// paired with: the code is refused before anything is sent.
func TestMeshPlaintextNotAllowed(t *testing.T) {
	b := startTestHubOpts(t, hubB, "Office", newMeshStore(), "", nil, nil, testHubOpts{plain: true, plainAllow: []string{"127.0.0.1"}})
	elsewhere := startTestHubOpts(t, hubA, "家里", newMeshStore(), "", nil, nil, testHubOpts{plainAllow: []string{"192.0.2.0/24"}})
	off := startTestHub(t, hubC, "Lab", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := b.h.newPeerCode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, mh := range []*testHub{elsewhere, off} {
		if _, err := mh.h.joinPeer(ctx, code.Code); !errors.Is(err, errPlainNotAllowed) {
			t.Fatalf("%s joined a plaintext hub: %v", mh.h.cfg.Name, err)
		}
	}
	// The code still works for a hub that may use it.
	ok := startTestHubOpts(t, hubC[:len(hubC)-1]+"4", "Allowed", newMeshStore(), "", nil, nil, testHubOpts{plainAllow: []string{"127.0.0.0/8"}})
	if _, err := ok.h.joinPeer(ctx, code.Code); err != nil {
		t.Fatalf("an allowed hub with TLS could not join over plaintext: %v", err)
	}
}

// A hub with neither TLS nor plaintext links cannot make a code.
func TestMeshPlaintextOffNeedsTLS(t *testing.T) {
	h := startTestHubOpts(t, hubA, "家里", newMeshStore(), "", nil, nil, testHubOpts{plain: true})
	if _, err := h.h.newPeerCode(context.Background()); !errors.Is(err, errNoTLS) {
		t.Fatalf("code without TLS: %v", err)
	}
}

// The gate and the two mesh handlers let plaintext through only for the mesh paths, from an allowed address that is
// not a container's gateway.
func TestPlainPeerRequest(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Mesh.Plaintext.Allow = []string{"198.51.100.0/24", "192.0.2.9"}
	gw := netip.MustParseAddr("192.0.2.9")
	h.admin.gateways = func() []netip.Addr { return []netip.Addr{gw} }
	for _, tc := range []struct {
		name, method, path, remote string
		tls                        bool
		status                     int
		code                       string
	}{
		{"allowed link", "GET", peerPath, "198.51.100.7:1", false, 401, ""}, // past the gate: the credential is wrong
		{"allowed pairing", "POST", peerPairPath, "198.51.100.7:1", false, 401, ""},
		{"other path", "GET", "/api/v1/machines", "198.51.100.7:1", false, 403, "tls_required"},
		{"not listed", "GET", peerPath, "203.0.113.5:1", false, 403, "tls_required"},
		{"gateway", "POST", peerPairPath, "192.0.2.9:1", false, 403, "tls_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := securityRequest(h.routes(), tc.method, tc.path, "invalid", "{}", tc.remote, tc.tls)
			requireCode(t, w, tc.status, tc.code)
			if tc.code == "" && strings.Contains(w.Body.String(), "tls_required") {
				t.Fatalf("refused for TLS: %s", w.Body.String())
			}
		})
	}
	// A bad entry turns plaintext off altogether rather than opening part of it.
	h.cfg.Mesh.Plaintext.Allow = []string{"198.51.100.0/24", "198.51.*.1"}
	if h.plainPeersOn() || h.plainPeerAllowed(netip.MustParseAddr("198.51.100.7")) {
		t.Fatal("a list with a typo allowed plaintext")
	}
}

func TestPlainEndpointsRankAndDial(t *testing.T) {
	got := rankEndpoints([]string{"http://[2001:db8::5]:18660/", "https://hub.example.com:18662", "http://hub.example.com:18660",
		"https://[2001:db8::5]:18662", "ftp://x", "http://[2001:db8::5]:18660"})
	want := []string{"https://[2001:db8::5]:18662", "https://hub.example.com:18662", "http://[2001:db8::5]:18660", "http://hub.example.com:18660"}
	if !slices.Equal(got, want) {
		t.Fatalf("ranked %q", got)
	}
	h := newTestHub(t)
	if d := h.peerDialable(got); !slices.Equal(d, want[:2]) {
		t.Fatalf("plaintext off still dials %q", d)
	}
	h.cfg.Mesh.Plaintext.Allow = []string{"2001:db8::/32"}
	if d := h.peerDialable(got); !slices.Equal(d, want) { // names are checked once resolved
		t.Fatalf("dialable %q", d)
	}
	h.cfg.Mesh.Plaintext.Allow = []string{"203.0.113.0/24"}
	if d := h.peerDialable(got); !slices.Equal(d, []string{want[0], want[1], want[3]}) {
		t.Fatalf("dialable %q", d)
	}
	if _, err := h.plainPeerDial(context.Background(), nil, "tcp", "[2001:db8::5]:18660"); !errors.Is(err, errPlainNotAllowed) {
		t.Fatalf("dialed an address not allowed: %v", err)
	}
	if u := peerWSURL("http://192.0.2.1:18660", peerPath); u != "ws://192.0.2.1:18660/api/v1/peer" {
		t.Fatal(u)
	}
	if u := peerWSURL("https://192.0.2.1:18662", peerPath); u != "wss://192.0.2.1:18662/api/v1/peer" {
		t.Fatal(u)
	}
	h.cfg.Mesh.Plaintext.Advertise = []string{"http://hub.example.com:18660/", "https://hub.example.com", "http://u@x:1", "http://x:1/path"}
	if e := h.plainEndpoints(); !slices.Equal(e, []string{"http://hub.example.com:18660"}) {
		t.Fatalf("advertised %q", e)
	}
}

func TestPlainRules(t *testing.T) {
	r, err := parsePlainRules([]string{"192.0.2.*", "198.51.*.*", "198.51.100.7", "2001:db8::/32", "*.Example.com", "hub.example.org."})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.0.0/16"),
		netip.MustParsePrefix("198.51.100.7/32"), netip.MustParsePrefix("2001:db8::/32")}
	if !slices.Equal(r.prefixes, want) || r.any || !slices.Equal(r.names, []string{"*.example.com", "hub.example.org"}) {
		t.Fatalf("parsed %+v", r)
	}
	for host, ok := range map[string]bool{"a.example.com": true, "a.b.EXAMPLE.com.": true, "example.com": false,
		"badexample.com": false, "hub.example.org": true, "x.hub.example.org": false} {
		if r.matchName(host) != ok {
			t.Errorf("%s matched: %v", host, !ok)
		}
	}
	for _, bad := range []string{"192.*.2.*", "192.0.2", "300.*.*.*", "*.com", "a_b.example.com", "*foo", "192.0.02.*"} {
		if _, err := parsePlainRules([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if r, _ := parsePlainRules([]string{"*"}); !r.any || !r.on() {
		t.Fatal("* is not everything")
	}
	h := newTestHub(t)
	h.cfg.Mesh.Plaintext.Allow = []string{"*"}
	gw := netip.MustParseAddr("192.0.2.10")
	h.admin.gateways = func() []netip.Addr { return []netip.Addr{gw} }
	if !h.plainPeerAllowed(netip.MustParseAddr("203.0.113.5")) || h.plainPeerAllowed(gw) {
		t.Fatal("* should allow any address but a container's gateway")
	}
	// Names are for dialing only: an incoming connection never matches one.
	h.cfg.Mesh.Plaintext.Allow = []string{"*.example.com"}
	if !h.plainPeersOn() || h.plainPeerAllowed(netip.MustParseAddr("203.0.113.5")) {
		t.Fatal("a name rule allowed an address")
	}
}

// A host name rule lets the dial go to whatever the name resolves to; without one the resolved address must be
// allowed itself.
func TestPlainPeerDialByName(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	h := newTestHub(t)
	d := &net.Dialer{Timeout: 2 * time.Second}
	ctx := context.Background()
	h.cfg.Mesh.Plaintext.Allow = []string{"192.0.2.0/24"}
	if _, err := h.plainPeerDial(ctx, d, "tcp", "localhost:"+port); !errors.Is(err, errPlainNotAllowed) {
		t.Fatalf("dialed a name resolving outside the list: %v", err)
	}
	h.cfg.Mesh.Plaintext.Allow = []string{"localhost"}
	c, err := h.plainPeerDial(ctx, d, "tcp", "localhost:"+port)
	if err != nil {
		t.Fatalf("a listed name: %v", err)
	}
	c.Close()
	if _, err := h.plainPeerDial(ctx, d, "tcp", "127.0.0.1:"+port); !errors.Is(err, errPlainNotAllowed) {
		t.Fatalf("a name rule allowed its address: %v", err)
	}
}
