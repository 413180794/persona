package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// newProxy is a proxy whose open internet is the test server at target: every name resolves to 192.0.2.1, which
// dials target (inside.example.com resolves to loopback).
func newProxy(t *testing.T, target string, upstream func(*url.URL) (*url.URL, error)) *Proxy {
	t.Helper()
	p := &Proxy{HubAddr: target, Upstream: upstream,
		Resolve: func(_ context.Context, name string) ([]net.IP, error) {
			if name == "inside.example.com" {
				return []net.IP{net.ParseIP("127.0.0.1")}, nil
			}
			return []net.IP{net.ParseIP("192.0.2.1")}, nil
		},
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			h, _, _ := net.SplitHostPort(addr)
			if h == "192.0.2.1" { // the open internet, played by the test server
				addr = target
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}}
	if upstream == nil {
		p.Upstream = func(*url.URL) (*url.URL, error) { return nil, nil }
	}
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func client(proxy string) *http.Client {
	u, _ := url.Parse(proxy)
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
}

func TestProxyAllowsOnlyItsHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello %s %s", r.Host, r.Header.Get("Proxy-Authorization"))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	var decided []string
	p := newProxy(t, addr, nil)
	p.OnDecision = func(ext, host string, ok bool) { decided = append(decided, fmt.Sprintf("%s %s %v", ext, host, ok)) }
	purl := p.Register("notes", Rule{Hosts: []string{"api.example.com", "*.cdn.example.net", "192.0.2.7"}})

	c := client(purl)
	get := func(u string) (int, string) {
		t.Helper()
		resp, err := c.Get(u)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("http://api.example.com/x"); code != 200 || !strings.HasPrefix(body, "hello api.example.com ") || strings.Contains(body, "Basic") {
		t.Fatalf("allowed host: %d %q (the proxy's credential must not go on)", code, body)
	}
	if code, _ := get("http://a.b.cdn.example.net/"); code != 200 {
		t.Fatalf("wildcard: %d", code)
	}
	if code, _ := get("http://cdn.example.net/"); code != 403 {
		t.Fatalf("*.cdn.example.net is not cdn.example.net itself: %d", code)
	}
	if code, body := get("http://evil.example.org/"); code != 403 || !strings.Contains(body, "permissions.network") {
		t.Fatalf("other host: %d %q", code, body)
	}
	if code, _ := get("http://192.0.2.8/"); code != 403 {
		t.Fatalf("an IP not listed: %d", code)
	}
	if code, _ := get("http://" + egressHub() + "/"); code != 403 {
		t.Fatalf("the hub without a token of it: %d", code)
	}
	st := p.Stats("notes")
	if st.Allowed != 2 || st.Denied != 4 || st.LastDenied == "" {
		t.Fatalf("counts %+v", st)
	}
	if len(decided) != 6 {
		t.Fatalf("decisions %v", decided)
	}
}

func egressHub() string { return HubHost }

func TestProxyNeedsItsCredential(t *testing.T) {
	p := newProxy(t, "127.0.0.1:1", nil)
	purl := p.Register("notes", Rule{Hosts: []string{"api.example.com"}})
	u, _ := url.Parse(purl)
	for _, bad := range []string{"http://" + p.Addr(), "http://ext:wrong@" + p.Addr(), "http://other:" + func() string { pw, _ := u.User.Password(); return pw }() + "@" + p.Addr()} {
		resp, err := client(bad).Get("http://api.example.com/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusProxyAuthRequired {
			t.Fatalf("%s: %d", bad, resp.StatusCode)
		}
	}
	// A new start: the old credential stops working.
	p.Register("notes", Rule{Hosts: []string{"api.example.com"}})
	resp, err := client(purl).Get("http://api.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("old credential: %d", resp.StatusCode)
	}
}

func TestProxyConnectAndHub(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tls ok") }))
	defer srv.Close()
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hub %s %s", r.URL.Path, r.Header.Get("Authorization"))
	}))
	defer hub.Close()
	p := newProxy(t, strings.TrimPrefix(srv.URL, "https://"), nil)
	p.HubAddr = strings.TrimPrefix(hub.URL, "http://")
	purl := p.Register("notes", Rule{Hosts: []string{"api.example.com"}, Hub: true})
	u, _ := url.Parse(purl)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(u)
	tr.TLSClientConfig.InsecureSkipVerify = true
	c := &http.Client{Transport: tr}
	resp, err := c.Get("https://api.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "tls ok" {
		t.Fatalf("CONNECT: %q", b)
	}
	if _, err := c.Get("https://evil.example.org/"); err == nil {
		t.Fatal("CONNECT to a host not listed went through")
	}
	req, _ := http.NewRequest("GET", "http://"+HubHost+"/api/v1/kb/search", nil)
	req.Header.Set("Authorization", "Bearer pext_x")
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hub /api/v1/kb/search Bearer pext_x" {
		t.Fatalf("hub: %q", b)
	}
}

func TestProxyRefusesNamesThatLeadInside(t *testing.T) {
	p := newProxy(t, "127.0.0.1:1", nil)
	purl := p.Register("notes", Rule{Hosts: []string{"inside.example.com"}})
	resp, err := client(purl).Get("http://inside.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(b), "loopback") {
		t.Fatalf("%d %q", resp.StatusCode, b)
	}
}

func TestProxyGoesOutThroughTheHubsProxy(t *testing.T) {
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.Method+" "+r.Host+" "+r.Header.Get("Proxy-Authorization"))
		if r.Method != http.MethodConnect {
			http.Error(w, "connect only", 400)
			return
		}
		c, buf, _ := w.(http.Hijacker).Hijack()
		defer c.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		_ = buf.Flush()
		req, err := http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			return
		}
		body := "via upstream " + req.Host
		fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	}))
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	upURL.User = url.UserPassword("u", "p")
	p := newProxy(t, "127.0.0.1:1", func(*url.URL) (*url.URL, error) { return upURL, nil })
	purl := p.Register("notes", Rule{Hosts: []string{"api.example.com"}})
	resp, err := client(purl).Get("http://api.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "via upstream api.example.com" || len(asked) != 1 || !strings.HasPrefix(asked[0], "CONNECT api.example.com:80 Basic ") {
		t.Fatalf("%q %v", b, asked)
	}
}

func TestProxyUnixSocketIsTheExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	p := newProxy(t, strings.TrimPrefix(srv.URL, "http://"), nil)
	p.Register("notes", Rule{Hosts: []string{"api.example.com"}})
	sock := filepath.Join(t.TempDir(), "proxy.sock")
	if err := p.ListenUnix("notes", sock); err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "sandbox"}),
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}}
	c := &http.Client{Transport: tr}
	resp, err := c.Get("http://api.example.com/") // no credential: the socket says who it is
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d", resp.StatusCode)
	}
	p.Unregister("notes")
	if resp, err := c.Get("http://api.example.com/"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden { // a connection kept from before: refused all the same
			t.Fatalf("after Unregister: %d", resp.StatusCode)
		}
	}
}

func TestMatches(t *testing.T) {
	for _, c := range []struct {
		host string
		ok   bool
	}{{"api.example.com", true}, {"API.example.com.", true}, {"x.api.example.com", false}, {"a.cdn.example.net", true},
		{"cdn.example.net", false}, {"evilcdn.example.net", false}} {
		if got := Matches(c.host, []string{"api.example.com", "*.cdn.example.net"}); got != c.ok {
			t.Errorf("%s: %v", c.host, got)
		}
	}
}
