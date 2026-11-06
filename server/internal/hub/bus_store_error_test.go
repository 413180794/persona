package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/db"
)

// flakyIdents is memIdents whose lookups fail while down is set (Postgres restarting).
type flakyIdents struct {
	memIdents
	down atomic.Bool
}

func (f *flakyIdents) BusIdentityByHash(ctx context.Context, hash string) (*db.BusIdentity, error) {
	if f.down.Load() {
		return nil, errors.New("connection refused")
	}
	return f.memIdents.BusIdentityByHash(ctx, hash)
}

// A store that does not answer is no verdict on a token: the bus answers 503 store_unavailable, a manager's client
// (StopWhenRevoked) keeps trying and connects once the store is back. Only the hub's explicit word stops it: a 401
// token_revoked for a bus token it has no identity for, or a hang-up with 4002; a token of another shape is a plain
// 401, rejected but not revoked.
func TestBusStoreErrorIsNotARevokedToken(t *testing.T) {
	h := newTestHub(t)
	idents := &flakyIdents{}
	h.bus.idents = idents
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tok := busToken(t, h, "m-abc123", "工作")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	idents.down.Store(true)
	req, _ := http.NewRequest("GET", srv.URL+bus.Path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Code string }
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || body.Code != bus.CodeStoreUnavailable {
		t.Fatalf("store down: %d %q", resp.StatusCode, body.Code)
	}
	if _, err := bus.Dial(ctx, srv.URL, tok, nil); err == nil || errors.Is(err, bus.ErrRejected) {
		t.Fatalf("a dial while the store is down: %v", err)
	}

	c := &bus.Client{Hub: srv.URL, Token: tok, HelloMethod: busHello, StopWhenRevoked: true,
		HelloParams: func() any { return map[string]any{"id": "m-abc123"} },
		Backoff:     10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond}
	ran := make(chan error, 1)
	go func() { ran <- c.Run(ctx) }()
	time.Sleep(300 * time.Millisecond) // several refused dials
	select {
	case err := <-ran:
		t.Fatalf("the client stopped while the store was down: %v", err)
	default:
	}
	idents.down.Store(false)
	if err := c.Wait(ctx); err != nil {
		t.Fatalf("not connected once the store was back: %v", err)
	}

	// Revoked: hung up with 4002, then 401 token_revoked: the client stops.
	if _, err := h.revokeBusToken(ctx, "m-abc123"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ran:
		if !errors.Is(err, bus.ErrRevoked) {
			t.Fatalf("Run after the revoke: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the client went on after its token was revoked")
	}
	if _, err := bus.Dial(ctx, srv.URL, tok, nil); !errors.Is(err, bus.ErrRevoked) {
		t.Fatalf("a revoked token's dial: %v", err)
	}
	if _, err := bus.Dial(ctx, srv.URL, "not-a-bus-token", nil); !errors.Is(err, bus.ErrRejected) || errors.Is(err, bus.ErrRevoked) {
		t.Fatalf("another shape of token: %v", err)
	}
}

// While the store is down the bus answers 503 without asking it again for a moment (one lookup for a burst of dials),
// logs one line a minute, and counts nothing against the source: two managers behind one address reconnecting
// through an outage must not lock that address (the user's phone on it too) out of the apps' API.
func TestBusStoreErrorIsThrottled(t *testing.T) {
	h := newTestHub(t)
	idents := &countingIdents{}
	idents.down.Store(true)
	h.bus.idents = idents
	h.cfg.Security.AuthFailures = 3
	var lines atomic.Int32
	h.logger = log.New(writerFunc(func(p []byte) (int, error) {
		if strings.Contains(string(p), "looking the token up") {
			lines.Add(1)
		}
		return len(p), nil
	}), "", 0)
	mux := http.NewServeMux()
	h.busRoutes(mux)
	for i := range 20 { // two managers, ten dials each, from one address
		w := securityRequest(mux, "GET", bus.Path, "jb_000"+string(rune('a'+i%2)), "", "198.51.100.1:1", true)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("dial %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	if n := idents.lookups.Load(); n != 1 {
		t.Fatalf("%d lookups for a burst of dials", n)
	}
	if n := lines.Load(); n != 1 {
		t.Fatalf("%d log lines", n)
	}
	r := httptest.NewRequest("GET", bus.Path, nil)
	r.RemoteAddr = "198.51.100.1:1"
	if retry := h.devtok.failures.retry(authSourceKey(r), time.Now(), h.cfg.Security); retry > 0 {
		t.Fatalf("the address is locked out of the apps' API for %s", retry)
	}
}

// countingIdents is flakyIdents counting its lookups.
type countingIdents struct {
	flakyIdents
	lookups atomic.Int32
}

func (c *countingIdents) BusIdentityByHash(ctx context.Context, hash string) (*db.BusIdentity, error) {
	c.lookups.Add(1)
	return c.flakyIdents.BusIdentityByHash(ctx, hash)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// slowIdents is memIdents whose lookups take a moment and give up when their context ends, as pgx does.
type slowIdents struct {
	memIdents
	cancelled atomic.Int32
}

func (s *slowIdents) BusIdentityByHash(ctx context.Context, hash string) (*db.BusIdentity, error) {
	select {
	case <-time.After(150 * time.Millisecond):
		return s.memIdents.BusIdentityByHash(ctx, hash)
	case <-ctx.Done():
		s.cancelled.Add(1)
		return nil, ctx.Err()
	}
}

// A client that hangs up mid-lookup (raw TCP, reset at once) neither cancels the lookup nor passes for a store error:
// the store being healthy, the bus never goes into its "store down" answer, and a manager dialing right after connects.
func TestBusHangUpMidLookupIsNotAStoreError(t *testing.T) {
	h := newTestHub(t)
	idents := &slowIdents{}
	h.bus.idents = idents
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tok := busToken(t, h, "m-abc123", "工作")
	addr := strings.TrimPrefix(srv.URL, "http://")
	for range 5 {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write([]byte("GET " + bus.Path + " HTTP/1.1\r\nHost: " + addr + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nAuthorization: Bearer " + tok + "\r\n\r\n"))
		time.Sleep(20 * time.Millisecond) // the lookup has begun
		_ = conn.(*net.TCPConn).SetLinger(0)
		_ = conn.Close()
	}
	time.Sleep(300 * time.Millisecond) // every lookup ran to its end
	if n := idents.cancelled.Load(); n != 0 {
		t.Fatalf("%d lookups cancelled by a client's hang-up", n)
	}
	if h.bus.storeDown.Load() != 0 {
		t.Fatal("a hang-up set the store-down answer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := bus.Dial(ctx, srv.URL, tok, nil)
	if err != nil {
		t.Fatalf("a manager's dial right after: %v", err)
	}
	_ = ws.CloseNow()
}
