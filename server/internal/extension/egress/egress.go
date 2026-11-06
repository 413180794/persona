// Package egress is the hub's proxy for extensions' outbound connections (docs/extensions.md "Egress"): each extension
// it knows reaches only the hosts of its permissions.network, the hub's own API under the name HubHost when it was
// granted any of it, and the hub machine's loopback under the name LocalHost when its permissions.network lists a
// loopback address. A sandboxed extension has no other way out; one outside a sandbox is pointed at it.
package egress

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// HubHost is the hub's own API as an extension behind the proxy reaches it (its loopback is not the hub's).
const HubHost = "hub.persona.internal"

// LocalHost is the hub machine's loopback as an extension behind the proxy reaches it: LocalHost:<port> is
// 127.0.0.1:<port> there. A sandbox's loopback is not the hub machine's, and HTTP clients (Go's net/http among them)
// never send a request for localhost or a loopback IP through a proxy, so a loopback address in permissions.network
// works only under this name.
const LocalHost = "host.persona.internal"

// loopbackNames are what permissions.network lists to let an extension reach LocalHost.
var loopbackNames = []string{"127.0.0.1", "::1", "localhost", LocalHost}

// User is the proxy's user name in an extension's proxy URL (the password tells which extension it is).
const User = "ext"

// Rule is what one extension may reach.
type Rule struct {
	// Hosts are permissions.network: names, or *.suffix for any name under it. An IP address passes only when it is
	// listed as it is.
	Hosts []string
	// Hub: it may reach HubHost (it holds a token of the hub's API).
	Hub bool
}

// Local says whether the rule lets its extension reach LocalHost: its permissions.network lists a loopback address
// (or LocalHost itself) as it is.
func (r Rule) Local() bool {
	return slices.ContainsFunc(r.Hosts, func(h string) bool {
		return slices.Contains(loopbackNames, strings.ToLower(strings.Trim(h, "[]")))
	})
}

// Counts are an extension's connections through the proxy.
type Counts struct {
	Allowed    int64     `json:"allowed"`
	Denied     int64     `json:"denied"`
	LastDenied string    `json:"last_denied,omitempty"`
	DeniedAt   time.Time `json:"denied_at,omitzero"`
}

// Proxy is the hub's egress proxy: one loopback TCP listener (an extension says who it is with its credential) and a
// unix socket per sandboxed extension (the socket says who it is).
type Proxy struct {
	// HubAddr is the hub's own listener (host:port) that HubHost stands for.
	HubAddr string
	// Upstream is the proxy the hub itself goes out through for u (nil, nil: straight). Default: the hub's
	// environment (HTTPS_PROXY / HTTP_PROXY / NO_PROXY).
	Upstream func(u *url.URL) (*url.URL, error)
	// OnDecision is told each connection allowed or denied (the hub's metrics, docs/extensions.md "Observability").
	OnDecision func(ext, host string, allowed bool)
	// Resolve and Dial: tests replace them.
	Resolve func(ctx context.Context, host string) ([]net.IP, error)
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Logf    func(string, ...any)

	mu     sync.Mutex
	ln     net.Listener
	rules  map[string]Rule
	creds  map[string]string // credential → extension
	credOf map[string]string // extension → its credential
	unix   map[string]net.Listener
	relay  map[string]net.Listener // ext → its socket that leads to one fixed unix socket (RelayUnix)
	counts map[string]*Counts
}

// Start listens on loopback (an unused port).
func (p *Proxy) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.ln = ln
	p.mu.Unlock()
	go p.serve(ln, "")
	return nil
}

// Addr is the loopback listener's host:port.
func (p *Proxy) Addr() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

// ownPort says whether port is the loopback listener's own (LocalHost must not lead back into the proxy).
func (p *Proxy) ownPort(port string) bool {
	_, own, err := net.SplitHostPort(p.Addr())
	return err == nil && own == port
}

// Close stops every listener.
func (p *Proxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		_ = p.ln.Close()
	}
	for _, l := range p.unix {
		_ = l.Close()
	}
	for _, l := range p.relay {
		_ = l.Close()
	}
	p.unix, p.relay = nil, nil
}

// Register sets ext's rule and gives it a new credential (each start of its program): the URL of the loopback
// listener with it, for HTTP(S)_PROXY.
func (p *Proxy) Register(ext string, r Rule) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	cred := hex.EncodeToString(b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rules == nil {
		p.rules, p.creds, p.credOf, p.counts = map[string]Rule{}, map[string]string{}, map[string]string{}, map[string]*Counts{}
	}
	if old, ok := p.credOf[ext]; ok {
		delete(p.creds, old)
	}
	p.rules[ext] = Rule{Hosts: slices.Clone(r.Hosts), Hub: r.Hub}
	p.creds[cred], p.credOf[ext] = ext, cred
	addr := ""
	if p.ln != nil {
		addr = p.ln.Addr().String()
	}
	return (&url.URL{Scheme: "http", User: url.UserPassword(User, cred), Host: addr}).String()
}

// ListenUnix serves ext on a unix socket at path (made anew; only this user may connect): its sandbox's way out.
func (p *Proxy) ListenUnix(ext, path string) error {
	p.mu.Lock()
	if old := p.unix[ext]; old != nil {
		_ = old.Close()
		delete(p.unix, ext)
	}
	p.mu.Unlock()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	p.mu.Lock()
	if p.unix == nil {
		p.unix = map[string]net.Listener{}
	}
	p.unix[ext] = ln
	p.mu.Unlock()
	go p.serve(ln, ext)
	return nil
}

// Unregister forgets ext: its credential and its socket stop working (switched off, removed).
func (p *Proxy) Unregister(ext string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cred, ok := p.credOf[ext]; ok {
		delete(p.creds, cred)
		delete(p.credOf, ext)
	}
	delete(p.rules, ext)
	if l := p.unix[ext]; l != nil {
		_ = l.Close()
		delete(p.unix, ext)
	}
	if l := p.relay[ext]; l != nil {
		_ = l.Close()
		delete(p.relay, ext)
	}
}

// RelayUnix serves ext on a unix socket at path (made anew; only this user may connect) whose every connection is
// joined to the unix socket at target, dialed then (so a target made anew, a restarted node's, is still reached):
// a sandboxed agent runtime's way to this machine's node (#5413), and to nothing else. Unregister closes it.
func (p *Proxy) RelayUnix(ext, path, target string) error {
	p.mu.Lock()
	if old := p.relay[ext]; old != nil {
		_ = old.Close()
		delete(p.relay, ext)
	}
	p.mu.Unlock()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	p.mu.Lock()
	if p.relay == nil {
		p.relay = map[string]net.Listener{}
	}
	p.relay[ext] = ln
	p.mu.Unlock()
	go p.serveRelay(ln, ext, target)
	return nil
}

func (p *Proxy) serveRelay(ln net.Listener, ext, target string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			var d net.Dialer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			up, err := d.DialContext(ctx, "unix", target)
			cancel()
			if err != nil {
				p.logf("egress: %s: %s: %v", ext, filepath.Base(target), err)
				return
			}
			defer up.Close()
			// Either end gone ends both: the protocol on it has no half-closed state.
			done := make(chan struct{}, 2)
			pipe := func(dst, src net.Conn) {
				_, _ = io.Copy(dst, src)
				done <- struct{}{}
			}
			go pipe(up, c)
			go pipe(c, up)
			<-done
		}()
	}
}

// Stats are ext's counts so far.
func (p *Proxy) Stats(ext string) Counts {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.counts[ext]; c != nil {
		return *c
	}
	return Counts{}
}

func (p *Proxy) serve(ln net.Listener, ext string) {
	srv := &http.Server{Handler: p.handler(ext), ReadHeaderTimeout: 30 * time.Second,
		ErrorLog: nil}
	_ = srv.Serve(ln)
}

func (p *Proxy) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}

// who is the extension a request on the loopback listener is from (its Proxy-Authorization).
func (p *Proxy) who(r *http.Request) string {
	auth := r.Header.Get("Proxy-Authorization")
	scheme, b64, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return ""
	}
	user, pass, _ := strings.Cut(string(raw), ":")
	if user != User {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.creds[pass]
}

func (p *Proxy) rule(ext string) (Rule, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.rules[ext]
	return r, ok
}

func (p *Proxy) count(ext, host string, allowed bool) {
	p.mu.Lock()
	if p.counts == nil {
		p.counts = map[string]*Counts{}
	}
	c := p.counts[ext]
	if c == nil {
		c = &Counts{}
		p.counts[ext] = c
	}
	if allowed {
		c.Allowed++
	} else {
		c.Denied++
		c.LastDenied, c.DeniedAt = host, time.Now()
	}
	p.mu.Unlock()
	if p.OnDecision != nil {
		p.OnDecision(ext, host, allowed)
	}
}

// Matches says whether host is one of the patterns: the name itself, or under a *.suffix one.
func Matches(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, pat := range patterns {
		if pat == host || strings.HasPrefix(pat, "*.") && strings.HasSuffix(host, pat[1:]) {
			return true
		}
	}
	return false
}

func (p *Proxy) handler(fixed string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := fixed
		if ext == "" {
			ext = p.who(r)
			if ext == "" {
				w.Header().Set("Proxy-Authenticate", `Basic realm="persona"`)
				http.Error(w, "persona: this proxy is for the hub's extensions", http.StatusProxyAuthRequired)
				return
			}
		}
		rule, ok := p.rule(ext)
		if !ok {
			http.Error(w, "persona: this extension is not running", http.StatusForbidden)
			return
		}
		hostport := r.Host
		if r.Method != http.MethodConnect {
			if r.URL.Host == "" || r.URL.Scheme != "http" {
				http.Error(w, "persona: only http:// requests and CONNECT", http.StatusBadRequest)
				return
			}
			hostport = r.URL.Host
		}
		host, port, err := net.SplitHostPort(hostport)
		if err != nil {
			host, port = hostport, "80"
			if r.Method == http.MethodConnect {
				port = "443"
			}
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		hub, local := host == HubHost, host == LocalHost
		var allowed bool
		switch {
		case hub:
			allowed = rule.Hub
		case local:
			allowed = rule.Local() && !p.ownPort(port)
		default:
			allowed = Matches(host, rule.Hosts)
		}
		p.count(ext, host, allowed)
		if !allowed {
			if local && rule.Local() {
				p.logf("extension %s: refused %s:%s (the proxy itself)", ext, host, port)
				http.Error(w, fmt.Sprintf("persona: %s:%s is this proxy", host, port), http.StatusForbidden)
				return
			}
			p.logf("extension %s: refused %s (not in its permissions.network)", ext, host)
			http.Error(w, fmt.Sprintf("persona: %s is not in this extension's permissions.network", host), http.StatusForbidden)
			return
		}
		ctx := r.Context()
		dial := func() (net.Conn, error) {
			switch {
			case hub:
				return p.dial(ctx, "tcp", p.HubAddr)
			case local:
				return p.dial(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
			}
			return p.dialOut(ctx, host, port, rule)
		}
		if r.Method == http.MethodConnect || isUpgrade(r) {
			p.tunnel(w, r, dial)
			return
		}
		p.forward(w, r, ext, dial)
	})
}

// farHost is the Host header a request for hostport goes on with: the hub's address for HubHost, the loopback (with
// the port as written) for LocalHost, else def.
func (p *Proxy) farHost(hostport, def string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = hostport, ""
	}
	switch strings.ToLower(host) {
	case HubHost:
		return p.HubAddr
	case LocalHost:
		if port == "" {
			return "127.0.0.1"
		}
		return net.JoinHostPort("127.0.0.1", port)
	}
	return def
}

func isUpgrade(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && r.Header.Get("Upgrade") != ""
}

func (p *Proxy) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if p.Dial != nil {
		return p.Dial(ctx, network, addr)
	}
	d := net.Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, network, addr)
}

// errInternal: a name of the open internet that leads to this machine or the cloud's metadata.
var errInternal = errors.New("resolves to a loopback or link-local address")

// dialOut reaches host:port: through the hub's upstream proxy when it has one for it, else straight — never to a
// loopback or link-local address the extension did not name as it is.
func (p *Proxy) dialOut(ctx context.Context, host, port string, rule Rule) (net.Conn, error) {
	upstream := p.Upstream
	if upstream == nil {
		upstream = fromEnvironment
	}
	target := net.JoinHostPort(host, port)
	if up, err := upstream(&url.URL{Scheme: "https", Host: target}); err == nil && up != nil {
		return p.connectVia(ctx, up, target)
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return p.dial(ctx, "tcp", target) // named as it is in its permissions (Matches)
	}
	resolve := p.Resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			ips := make([]net.IP, 0, len(addrs))
			for _, a := range addrs {
				ips = append(ips, a.IP)
			}
			return ips, err
		}
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error = fmt.Errorf("%s: no address", host)
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() {
			last = fmt.Errorf("%s %w", host, errInternal)
			continue
		}
		c, err := p.dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// fromEnvironment is the hub's own proxy for u, from its environment.
func fromEnvironment(u *url.URL) (*url.URL, error) {
	return http.ProxyFromEnvironment(&http.Request{URL: u})
}

// connectVia opens a tunnel to target through the upstream proxy up.
func (p *Proxy) connectVia(ctx context.Context, up *url.URL, target string) (net.Conn, error) {
	addr := up.Host
	if up.Port() == "" {
		addr = net.JoinHostPort(up.Hostname(), "80")
	}
	c, err := p.dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: http.Header{}}
	if up.User != nil {
		pass, _ := up.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(up.User.Username()+":"+pass)))
	}
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	if err := req.Write(c); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		c.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		c.Close()
		return nil, fmt.Errorf("the hub's proxy refused %s: %s", target, resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: c, r: br}, nil
	}
	return c, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// CloseWrite half-closes the connection underneath, so a tunnel through the hub's proxy keeps a half-close too.
func (b *bufferedConn) CloseWrite() error { return closeWrite(b.Conn) }

// closeWrite shuts the write side of c, when c can (a TCP or Unix connection).
func closeWrite(c net.Conn) error {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return errors.ErrUnsupported
}

// tunnel is CONNECT (and an upgrade on plain HTTP: a WebSocket): the bytes both ways once the far end is reached.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request, dial func() (net.Conn, error)) {
	far, err := dial()
	if err != nil {
		http.Error(w, "persona: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		far.Close()
		http.Error(w, "persona: cannot tunnel", http.StatusInternalServerError)
		return
	}
	near, buf, err := hj.Hijack()
	if err != nil {
		far.Close()
		return
	}
	if r.Method == http.MethodConnect {
		_, err = near.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	} else {
		out := r.Clone(context.Background())
		out.RequestURI = ""
		out.URL = &url.URL{Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
		out.Host = p.farHost(r.URL.Host, out.Host)
		out.Header.Del("Proxy-Authorization")
		out.Header.Del("Proxy-Connection")
		err = out.Write(far)
	}
	if err != nil {
		near.Close()
		far.Close()
		return
	}
	// The extension ending its side (EOF) only half-closes the far end: the reply still on its way comes back, as
	// the hub's other relays keep a half-close. Both close once the far end is done, or on an error either way.
	var once sync.Once
	done := func() { near.Close(); far.Close() }
	go func() {
		if buf != nil && buf.Reader.Buffered() > 0 {
			b, _ := buf.Reader.Peek(buf.Reader.Buffered())
			if _, err := far.Write(b); err != nil {
				once.Do(done)
				return
			}
		}
		if _, err := io.Copy(far, near); err != nil || closeWrite(far) != nil {
			once.Do(done)
		}
	}()
	_, _ = io.Copy(near, far)
	once.Do(done)
}

// hop are the headers of one hop, not passed on.
var hop = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Te",
	"Trailer", "Transfer-Encoding", "Upgrade"}

// forward is a plain http:// request of extension ext: sent on over a connection dial makes, the answer back.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, ext string, dial func() (net.Conn, error)) {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           func(context.Context, string, string) (net.Conn, error) { return dial() },
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 2 * time.Minute,
	}
	defer tr.CloseIdleConnections()
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hop {
		out.Header.Del(h)
	}
	if h := p.farHost(out.URL.Host, ""); h != "" {
		out.URL.Host, out.Host = h, h
	}
	resp, err := tr.RoundTrip(out)
	if err != nil {
		http.Error(w, "persona: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range hop {
		resp.Header.Del(h)
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	body := &readErr{r: resp.Body}
	if _, err := io.Copy(w, body); err != nil {
		// Returning normally would let net/http end a chunked answer cleanly, handing the extension a
		// truncated body as a complete one; aborting drops the connection so its client sees the cut.
		// Only a failed read is an upstream cut-off; a failed write or a canceled request means the
		// extension's own client gave up, which is not worth a log line.
		if body.err != nil && r.Context().Err() == nil {
			p.logf("extension %s: %s: answer cut off: %v", ext, r.URL.Host, body.err)
		}
		panic(http.ErrAbortHandler)
	}
}

// readErr remembers the first read error other than io.EOF, so a copy failure can be told apart
// as a read (upstream) or a write (client) failure.
type readErr struct {
	r   io.Reader
	err error
}

func (e *readErr) Read(b []byte) (int, error) {
	n, err := e.r.Read(b)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}
