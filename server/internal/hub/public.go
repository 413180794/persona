package hub

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/netaddr"
)

type publicState struct {
	ddns        *ddnsUpdater
	mu          sync.RWMutex
	refreshMu   sync.Mutex
	id          string
	port        int
	fp          string
	certificate *tls.Certificate
	server      *http.Server
	listener    net.Listener
	addresses   addressSource
	endpoints   []Endpoint
}

func (h *Hub) initHubID(ctx context.Context) error {
	h.public.mu.Lock()
	defer h.public.mu.Unlock()
	if h.public.id != "" {
		return nil
	}
	var id string
	if h.store != nil {
		if _, err := h.store.Setting(ctx, "hub_id", &id); err != nil {
			return fmt.Errorf("load hub id: %w", err)
		}
	}
	if id == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
		id = fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
		if h.store != nil {
			if err := h.store.PutSetting(ctx, "hub_id", id); err != nil {
				return fmt.Errorf("save hub id: %w", err)
			}
		}
	}
	h.public.id = id
	return nil
}

// startPublic prepares both transports before either one accepts requests.
func (h *Hub) startPublic(ctx context.Context) error {
	if err := h.initHubID(ctx); err != nil {
		return err
	}
	if h.cfg.Public.TLSListen != "" {
		if err := h.refreshTLS(time.Now()); err != nil {
			return err
		}
		ln, activated, err := listenTCP(h.cfg.Public.TLSListen)
		if err != nil {
			return fmt.Errorf("listen TLS: %w", err)
		}
		if activated {
			h.logf("hub TLS: listening on the socket systemd passed (%s)", ln.Addr())
		}
		h.public.listener = ln
		h.public.server = newHubHTTPServer(h.cfg.Public.TLSListen, nil)
		h.public.server.ErrorLog = log.New(&handshakeNoise{logf: h.logf}, "", 0)
		h.public.server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				h.public.mu.RLock()
				defer h.public.mu.RUnlock()
				return h.public.certificate, nil
			},
		}
		h.public.mu.Lock()
		h.public.port = ln.Addr().(*net.TCPAddr).Port
		h.public.mu.Unlock()
		port, fp := h.tlsPin()
		h.logf("hub TLS listening on port %d, SPKI SHA-256 %s", port, fp)
	}
	port, _ := h.tlsPin()
	h.public.mu.Lock()
	h.public.endpoints = collectEndpoints(h.cfg, port, nil, time.Now())
	h.public.mu.Unlock()
	h.startDDNS(ctx)
	if err := h.refreshEndpoints(time.Now()); err != nil {
		h.logf("discover public endpoints: %v", err)
	}
	go h.publicLoop(ctx)
	return nil
}

func (h *Hub) refreshTLS(now time.Time) error {
	cfg := h.cfg.Public
	if cfg.TLSListen == "" {
		return nil
	}
	var cert tls.Certificate
	var fp string
	var err error
	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return fmt.Errorf("public.cert_file and public.key_file must be set together")
		}
		cert, fp, err = loadTLSFiles(cfg.CertFile, cfg.KeyFile)
	} else {
		if h.cfg.dir == "" {
			return fmt.Errorf("hub data directory is required for managed TLS")
		}
		cert, fp, err = loadOrCreateTLS(filepath.Join(h.cfg.dir, "tls"), h.tlsNames(), now)
	}
	if err != nil {
		return fmt.Errorf("load hub TLS identity: %w", err)
	}
	h.public.mu.Lock()
	defer h.public.mu.Unlock()
	// A replacement key requires a restart and pairing again, never an unnoticed live rotation.
	if h.public.fp != "" && h.public.fp != fp {
		return fmt.Errorf("TLS identity key changed; restart the hub and pair devices again")
	}
	h.public.certificate, h.public.fp = &cert, fp
	return nil
}

func (h *Hub) publicLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	// Address changes (a new IPv6 prefix after the line redials) go out at once rather than at the next tick.
	var changed <-chan struct{}
	if h.public.addresses == nil {
		changed = netaddr.Watch(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			if err := h.refreshEndpoints(time.Now()); err != nil {
				h.logf("refresh public endpoints: %v", err)
			}
		case now := <-ticker.C:
			if err := h.refreshTLS(now); err != nil {
				h.logf("refresh public TLS: %v", err)
			}
			if err := h.refreshEndpoints(now); err != nil {
				h.logf("refresh public endpoints: %v", err)
			}
		}
	}
}

func newHubHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

func (h *Hub) serveHTTP(ctx context.Context) error {
	// Keep request contexts alive until a close frame has been sent. Cancelling a
	// websocket Read context first closes the transport without a close handshake.
	requestCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	if h.shutdown == nil {
		h.shutdown = newShutdownBudget(h.cfg)
		defer h.shutdown.close()
	}
	var servers []*http.Server
	var listeners []net.Listener
	if h.public.listener != nil {
		listeners = append(listeners, h.public.listener)
	}
	stopSessions, stopTests := func() {}, func() {}
	defer func() {
		h.shutdown.start()
		// Close both listeners before draining any clients, including pending upgrades.
		for _, ln := range listeners {
			_ = ln.Close()
		}
		h.shutdown.step(h, "websockets", h.stopWebSockets)
		cancel()
		h.shutdown.step(h, "HTTP handlers", func(sctx context.Context) error {
			var wg sync.WaitGroup
			for _, srv := range servers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := srv.Shutdown(sctx); err != nil {
						h.logf("shutdown HTTP server: %v", err)
						_ = srv.Close()
					}
				}()
			}
			wg.Wait()
			return sctx.Err()
		})
		h.shutdown.step(h, "device sessions", func(context.Context) error { stopSessions(); return nil })
		h.shutdown.step(h, "lock tests", func(context.Context) error { stopTests(); return nil })
	}()
	var err error
	stopSessions, err = h.startDeviceSessions(ctx)
	if err != nil {
		stopSessions = func() {}
		return err
	}
	stopTests, err = h.startLockTests(ctx)
	if err != nil {
		stopTests = func() {}
		return err
	}
	handler := h.routes()
	plain := newHubHTTPServer(h.cfg.Listen, handler)
	plain.BaseContext = func(net.Listener) context.Context { return requestCtx }
	errs := make(chan error, 2)
	if srv := h.public.server; srv != nil {
		srv.Handler = handler
		srv.BaseContext = plain.BaseContext
		servers = append(servers, srv)
		go func() { errs <- srv.ServeTLS(h.public.listener, "", "") }()
	}
	ln, activated, err := listenTCP(plain.Addr)
	if err != nil {
		return err
	}
	if activated {
		h.logf("hub: listening on the socket systemd passed (%s): restarts refuse no connection", ln.Addr())
	}
	servers = append(servers, plain)
	listeners = append(listeners, ln)
	go func() { errs <- plain.Serve(ln) }()
	h.logf("jarvisd hub %s listening on %s (%s)", h.version, h.cfg.Listen, h.cfg.PublicURL)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (h *Hub) publicRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/endpoints", h.serveEndpoints)
}

// hubID also works for hubs built without a database in unit tests.
func (h *Hub) hubID() string {
	if h.store == nil {
		_ = h.initHubID(context.Background())
	}
	h.public.mu.RLock()
	defer h.public.mu.RUnlock()
	return h.public.id
}

// tlsPin uses base64url without padding of SHA-256 over SubjectPublicKeyInfo DER.
func (h *Hub) tlsPin() (port int, fp string) {
	h.public.mu.RLock()
	defer h.public.mu.RUnlock()
	if h.public.port == 0 {
		return 0, ""
	}
	return h.public.port, h.public.fp
}

// pairCandidates keeps the QR small and never includes plaintext or LAN addresses.
func (h *Hub) pairCandidates() []string {
	var urls []string
	for _, endpoint := range h.endpointList() {
		if endpoint.Scheme == "https" && endpoint.Kind != "lan" {
			urls = append(urls, endpoint.URL)
			if len(urls) == 2 {
				break
			}
		}
	}
	return urls
}

// handshakeNoise keeps the TLS server's error log readable on a port the internet can reach: scanners and clients
// with a stale pin fail handshakes all day, one line each. Those lines are summed up at most every 10 minutes;
// anything else the server logs goes through unchanged.
type handshakeNoise struct {
	logf  func(string, ...any)
	mu    sync.Mutex
	count int
	since time.Time
	now   func() time.Time // tests
}

const handshakeSummaryEvery = 10 * time.Minute

func (n *handshakeNoise) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if !strings.Contains(line, "TLS handshake error") {
		n.logf("tls: %s", line)
		return len(p), nil
	}
	now := time.Now()
	if n.now != nil {
		now = n.now()
	}
	n.mu.Lock()
	if n.since.IsZero() {
		n.since = now
	}
	n.count++
	var count int
	var since time.Time
	if now.Sub(n.since) >= handshakeSummaryEvery {
		count, since = n.count, n.since
		n.count, n.since = 0, now
	}
	n.mu.Unlock()
	if count > 0 {
		n.logf("tls: %d failed handshakes since %s (scanners, or a client with an old pin)", count, since.Format(time.TimeOnly))
	}
	return len(p), nil
}
