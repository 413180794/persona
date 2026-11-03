package hub

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

type countedTokenStore struct {
	*tokenStore
	lookups atomic.Int32
}

func (s *countedTokenStore) DeviceTokenByHash(ctx context.Context, hash string) (*db.DeviceToken, error) {
	s.lookups.Add(1)
	return s.tokenStore.DeviceTokenByHash(ctx, hash)
}

func TestAuthRateLimitBeforeDatabase(t *testing.T) {
	h := newTestHub(t)
	s := &countedTokenStore{tokenStore: &tokenStore{rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}}
	h.store = s
	h.cfg.Security = SecurityConfig{AuthFailures: 2, AuthBlockSeconds: 120}
	mux := h.routes()
	for i, remote := range []string{"[2001:db8:1::1]:1", "[2001:db8:1::abcd]:2"} {
		w := securityRequest(mux, "GET", "/api/v1/ws", fmt.Sprintf("jd_invalid_%d", i), "", remote, true)
		requireCode(t, w, 401, "")
	}
	before := s.lookups.Load()
	w := securityRequest(mux, "GET", "/api/v1/machines", "jp_not_in_cache", "", "[2001:db8:1::ffff]:3", true)
	requireCode(t, w, 429, "rate_limited")
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > 120 {
		t.Fatalf("Retry-After %q", w.Header().Get("Retry-After"))
	}
	if s.lookups.Load() != before {
		t.Fatal("blocked request queried the database")
	}
	// A different /64 is independent, and a healthy device works over public TLS.
	w = securityRequest(mux, "GET", "/api/v1/machines", "jd_another", "", "[2001:db8:2::1]:1", true)
	requireCode(t, w, 401, "")
	if s.lookups.Load() != before+1 {
		t.Fatal("different /64 was blocked")
	}
	if err := s.CreateDeviceToken(context.Background(), db.DeviceToken{ID: "dev_test", Role: roleFull}, hashToken("jd_valid")); err != nil {
		t.Fatal(err)
	}
	w = securityRequest(mux, "GET", "/api/v1/machines", "jd_valid", "", "198.51.100.1:1", true)
	requireCode(t, w, 200, "")
	// IPv4 aggregates by address, not by /24 or port.
	for i := 0; i < 2; i++ {
		requireCode(t, securityRequest(mux, "GET", "/api/v1/ws", "bad", "", "198.51.100.2:1", true), 401, "")
	}
	requireCode(t, securityRequest(mux, "GET", "/api/v1/ws", "bad", "", "198.51.100.2:2", true), 429, "rate_limited")
	requireCode(t, securityRequest(mux, "GET", "/api/v1/ws", "bad", "", "198.51.100.3:1", true), 401, "")
}

func TestAuthLocalSourcesAreNotCounted(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Security = SecurityConfig{TrustedCIDRs: []string{"192.0.2.0/24"}, AuthFailures: 1}
	h.devtok.vpnLocal = func(netip.Addr) bool { return true }
	mux := h.routes()
	private := netip.AddrFrom4([4]byte{10, 0, 0, 1}).String() + ":1"
	for _, remote := range []string{"127.0.0.1:1", "[::1]:1", "192.0.2.1:1", private, "[fd00::1]:1", "[fe80::1]:1"} {
		for i := 0; i < 3; i++ {
			requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", "bad", "", remote, false), 401, "")
		}
	}
	cgnat := netip.AddrFrom4([4]byte{100, 64, 0, 1})
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/api/v1/machines", nil)
		r.RemoteAddr = net.JoinHostPort(cgnat.String(), "1")
		r.Header.Set("Authorization", "Bearer bad")
		local := &net.TCPAddr{IP: net.ParseIP("192.0.2.2"), Port: 1}
		r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, local))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		requireCode(t, w, 401, "")
	}
	if len(h.devtok.failures.buckets) != 0 {
		t.Fatal("local failures were counted")
	}
}

func TestAuthLimiterWindowExpiryAndCapacity(t *testing.T) {
	l := authLimiter{}
	cfg := SecurityConfig{AuthFailures: 2, AuthWindowSeconds: 10, AuthBlockSeconds: 20, AuthMaxSources: 2}
	now := time.Now()
	l.fail("a", now, cfg)
	l.fail("a", now.Add(11*time.Second), cfg)
	if l.retry("a", now.Add(11*time.Second), cfg) != 0 {
		t.Fatal("window did not reset")
	}
	l.fail("a", now.Add(12*time.Second), cfg)
	if l.retry("a", now.Add(13*time.Second), cfg) != 19*time.Second {
		t.Fatal("wrong block duration")
	}
	l.fail("b", now.Add(13*time.Second), cfg)
	// A full table does not lock new sources out (TestAuthLimiterFullTableMakesRoom).
	if l.retry("c", now.Add(14*time.Second), cfg) != 0 {
		t.Fatal("a full table refused a new source")
	}
	if l.retry("a", now.Add(33*time.Second), cfg) != 0 {
		t.Fatal("block did not expire")
	}
	if l.retry("c", now.Add(33*time.Second), cfg) != 0 {
		t.Fatal("expired entries still fill the table")
	}
}

func TestNegativeTokenCacheBound(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Security.AuthCacheEntries = 8
	h.store = &tokenStore{rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	for i := 0; i < 40; i++ {
		if _, ok := h.authenticate(context.Background(), fmt.Sprintf("jd_bad_%d", i)); ok {
			t.Fatal("invalid token accepted")
		}
		if len(h.devtok.cache) > 8 {
			t.Fatal("negative cache is unbounded")
		}
	}
	for key, c := range h.devtok.cache {
		c.at = time.Now().Add(-tokenCacheFor - time.Second)
		h.devtok.cache[key] = c
	}
	_, _ = h.authenticate(context.Background(), "jd_after_expiry")
	if len(h.devtok.cache) != 1 {
		t.Fatal("expired entries were not cleared")
	}
}

func TestSecurityLogsAreMaskedAndLimited(t *testing.T) {
	h := newTestHub(t)
	var logs bytes.Buffer
	h.logger = log.New(&logs, "", 0)
	mux := h.routes()
	for i := 0; i < 4; i++ {
		requireCode(t, securityRequest(mux, "GET", "/api/v1/ws?token=secret", "jd_secret", "", "[2001:db8:3::abcd]:1", true), 401, "")
	}
	if strings.Count(logs.String(), "auth source=") != 1 || !strings.Contains(logs.String(), "source=2001:db8:3::/64 path=/api/v1/ws") {
		t.Fatal(logs.String())
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "abcd") {
		t.Fatal("credential or host leaked")
	}
	// HTTP failures must not be logged a second time by logRequests.
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", "bad", "", "198.51.100.1:1", true), 401, "")
	if strings.Count(logs.String(), "\n") != 1 {
		t.Fatal(logs.String())
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.123:1"
	if maskedSource(r) != "192.0.2.0/24" {
		t.Fatal(maskedSource(r))
	}
}

func TestAuthLimiterFullTableMakesRoom(t *testing.T) {
	cfg := SecurityConfig{AuthFailures: 2, AuthMaxSources: 2, AuthBlockSeconds: 600}
	var l authLimiter
	now := time.Now()
	// Two blocked prefixes fill the table; b was blocked a second later, so its block ends later.
	for i, key := range []string{"2001:db8:a::/64", "2001:db8:b::/64"} {
		at := now.Add(time.Duration(i) * time.Second)
		l.fail(key, at, cfg)
		l.fail(key, at, cfg)
	}
	// A phone from a new prefix is not refused just because the table is full.
	if retry := l.retry("2001:db8:c::/64", now, cfg); retry != 0 {
		t.Fatalf("new source waits %v", retry)
	}
	// Its failure makes room (the blocked bucket that ends first goes), and blocked sources stay blocked otherwise.
	l.fail("2001:db8:c::/64", now.Add(time.Second), cfg)
	if len(l.buckets) != 2 {
		t.Fatalf("buckets = %d", len(l.buckets))
	}
	if l.retry("2001:db8:b::/64", now, cfg) == 0 {
		t.Fatal("the later blocked source was evicted")
	}
	// An unblocked bucket is evicted before any blocked one.
	l.fail("2001:db8:d::/64", now.Add(2*time.Second), cfg)
	if _, ok := l.buckets["2001:db8:c::/64"]; ok {
		t.Fatal("unblocked bucket kept over a blocked one")
	}
	if l.retry("2001:db8:b::/64", now, cfg) == 0 {
		t.Fatal("blocked source evicted while an unblocked one existed")
	}
}
