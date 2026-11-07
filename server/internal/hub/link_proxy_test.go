package hub

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/link"
)

// A link request without TLS: from this machine, or from the LAN when a reverse proxy says it took it over HTTPS;
// never from the internet, whatever the headers say (docs/operations/link.md "Hub behind a reverse proxy").
func TestPlainLinkOnlyThroughAProxy(t *testing.T) {
	h, _, _ := newLinkHub(t)
	token, _ := linkToken(t, h, "laptop")
	req := func(remote string, header map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", link.Path, nil)
		r.RemoteAddr = remote
		r.Header.Set("Authorization", "Bearer "+token)
		for k, v := range header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.routes().ServeHTTP(w, r)
		return w
	}
	tlsRequired := func(w *httptest.ResponseRecorder) bool {
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code == http.StatusForbidden && body["code"] == "tls_required"
	}
	for _, tc := range []struct {
		name, remote string
		header       map[string]string
		refused      bool
	}{
		{"LAN, no proxy", "[fd00::20]:5000", nil, true},
		{"LAN, a proxy that took plain HTTP", "[fd00::20]:5000", map[string]string{"X-Forwarded-Proto": "http"}, true},
		{"LAN, through an HTTPS proxy", "[fd00::20]:5000", map[string]string{"X-Forwarded-Proto": "https"}, false},
		{"LAN, Forwarded proto=https", "[fd00::21]:5000", map[string]string{"Forwarded": `for=192.0.2.1;proto=https`}, false},
		{"loopback (a proxy beside the hub)", "127.0.0.1:5000", nil, false},
		{"internet, claiming HTTPS", "203.0.113.9:5000", map[string]string{"X-Forwarded-Proto": "https"}, true},
	} {
		if w := req(tc.remote, tc.header); tlsRequired(w) != tc.refused {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
	// The apps' API from the internet stays refused in plaintext whatever the headers say.
	r := httptest.NewRequest("GET", "/api/v1/machines", nil)
	r.RemoteAddr, r.Header = "203.0.113.9:5000", http.Header{"X-Forwarded-Proto": {"https"}, "Authorization": {"Bearer " + h.cfg.Token}}
	w := httptest.NewRecorder()
	h.routes().ServeHTTP(w, r)
	requireCode(t, w, http.StatusForbidden, "tls_required")
}

// connTracker remembers the connections a listener accepted, so a test can drop them all (a proxy restarting).
type connTracker struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (l *connTracker) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, c)
		l.mu.Unlock()
	}
	return c, err
}

func (l *connTracker) drop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
}

// The hub without TLS of its own behind a reverse proxy at /prefix (TLS with a test CA, the prefix stripped,
// WebSockets passed on, the proxy on another machine of the LAN): persona-link pairs with a link without fp, connects,
// the hub reaches the resident node through it, and after the proxy drops every connection the link comes back.
func TestLinkThroughAReverseProxy(t *testing.T) {
	h, m, _ := newLinkHub(t) // the manager keeps using the hub's TLS test server
	h.linkPing = [2]time.Duration{200 * time.Millisecond, 2 * time.Second}

	// The hub's plain port, as the proxy reaches it: from a LAN address, not loopback, so the hub's check of the
	// forwarded scheme is what lets the link in.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "[fd00::20]:5000"
		h.routes().ServeHTTP(w, r)
	}))
	t.Cleanup(plain.Close)
	upstream, _ := url.Parse(plain.URL)
	var proxied sync.Map
	rp := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(upstream)
		pr.SetXForwarded() // X-Forwarded-Proto: https, as nginx's proxy_set_header does
		pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/prefix")
		pr.Out.URL.RawPath = ""
		proxied.Store(pr.Out.URL.Path, true)
	}, FlushInterval: -1}
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix" && !strings.HasPrefix(r.URL.Path, "/prefix/") {
			http.NotFound(w, r)
			return
		}
		rp.ServeHTTP(w, r)
	}))
	tracker := &connTracker{Listener: proxy.Listener}
	proxy.Listener = tracker
	proxy.StartTLS()
	t.Cleanup(proxy.Close)
	pool := x509.NewCertPool()
	pool.AddCert(proxy.Certificate()) // the proxy's CA as the "system" CAs: not the keychain
	t.Cleanup(link.UseRootsForTesting(pool))

	// The hub has no TLS listener; public_url is the proxy's.
	h.public.mu.Lock()
	h.public.port, h.public.fp = 0, ""
	h.public.endpoints = []Endpoint{{URL: proxy.URL + "/prefix", Kind: "lan", Port: 0, Scheme: "https"}}
	h.public.mu.Unlock()
	h.cfg.PublicURL = proxy.URL + "/prefix"

	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, `{"machine":"laptop"}`, "127.0.0.1:1", false)
	requireCode(t, w, http.StatusOK, "")
	var p pairCode
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.FP != "" || strings.Contains(p.PairURL, "fp=") || p.URL != proxy.URL+"/prefix" {
		t.Fatalf("pair code %+v", p)
	}
	path := filepath.Join(t.TempDir(), "link.json")
	if _, err := link.Pair(context.Background(), p.PairURL, "", "", path); err != nil {
		t.Fatal(err)
	}
	saved, err := link.Load(path)
	if err != nil || !saved.SystemTrust() || saved.FP != "" || len(saved.Endpoints) != 1 || saved.Endpoints[0].URL != proxy.URL+"/prefix" {
		t.Fatalf("saved %+v, %v", saved, err)
	}

	statuses, _, _ := runLink(t, *saved, fakeResidentNode(t))
	st := waitStatus(t, statuses, "connected through the proxy", func(s link.Status) bool { return s.State == "connected" && s.Node == "online" })
	if st.Trust != link.TrustSystem || st.Address != proxy.URL+"/prefix" {
		t.Fatalf("status %+v", st)
	}
	eventually(t, 10*time.Second, "machine online", func() bool { return m.snapshot().Online })
	first := pid(t, m)
	for _, p := range []string{link.Path, link.StreamPath} {
		if _, ok := proxied.Load(p); !ok {
			t.Fatalf("%s did not go through the proxy", p)
		}
	}

	// The proxy drops every connection (it restarted): the link notices and comes back through it.
	tracker.drop()
	waitStatus(t, statuses, "the link down", func(s link.Status) bool { return s.State != "connected" })
	waitStatus(t, statuses, "connected again", func(s link.Status) bool { return s.State == "connected" && s.Node == "online" })
	eventually(t, 10*time.Second, "machine online again", func() bool { return m.snapshot().Online })
	var again int
	eventually(t, 10*time.Second, "a call over the new link", func() bool {
		var out struct {
			PID int `json:"pid"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.call(ctx, "ping", map[string]any{}, &out); err != nil {
			return false
		}
		again = out.PID
		return true
	})
	if again == first {
		t.Fatalf("the call went over the old stream (pid %d)", again)
	}
}
