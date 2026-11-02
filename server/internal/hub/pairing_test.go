package hub

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
	"github.com/coder/websocket"
)

type pairStore struct {
	tokenStore
	pairs map[string]db.PairToken
}

func newPairStore() *pairStore {
	return &pairStore{tokenStore: tokenStore{rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}, pairs: map[string]db.PairToken{}}
}

func (s *pairStore) CreatePairToken(_ context.Context, hash string, expires time.Time, machine string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairs[hash] = db.PairToken{Hash: hash, CreatedAt: time.Now(), ExpiresAt: expires, MachineID: machine}
	return nil
}

func (s *pairStore) PairTokenByHash(_ context.Context, hash string) (*db.PairToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[hash]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (s *pairStore) RedeemPairToken(_ context.Context, hash string, device db.DeviceToken, tokenHash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pairs[hash]
	if !ok || p.UsedAt != nil || !time.Now().Before(p.ExpiresAt) {
		return false, nil
	}
	now := time.Now()
	p.UsedAt, p.DeviceID = &now, device.ID
	s.pairs[hash], s.rows[tokenHash] = p, device
	return true, nil
}

func newPairCode(t *testing.T, h *Hub) pairCode {
	t.Helper()
	w := securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, "", "127.0.0.1:1", false)
	requireCode(t, w, 200, "")
	var p pairCode
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPairTokenLifecycle(t *testing.T) {
	h := newTestHub(t)
	s := newPairStore()
	h.store = s
	h.cfg.PublicURL = "http://example.com:18660"
	p := newPairCode(t, h)
	if !strings.HasPrefix(p.Token, "jp_") || len(p.Token) != 67 || p.Token == h.cfg.Token {
		t.Fatal("bad pairing credential")
	}
	if left := time.Until(p.ExpiresAt); left > pairLifetime || left < pairLifetime-time.Second {
		t.Fatal("bad expiry")
	}
	if len(s.pairs) != 1 || s.pairs[hashToken(p.Token)].Hash != hashToken(p.Token) {
		t.Fatal("pair credential not persisted by hash")
	}
	u, err := url.Parse(p.PairURL)
	if err != nil || u.Query().Get("token") != p.Token || u.Query().Get("url") != h.cfg.PublicURL {
		t.Fatal("bad pairing URL")
	}
	for _, key := range []string{"hub", "fp", "tls", "alt"} {
		if u.Query().Has(key) {
			t.Fatalf("empty %s must be omitted", key)
		}
	}
	if who, ok := h.authenticate(context.Background(), p.Token); !ok || who.ID == "" || who.PairHash == "" {
		t.Fatal("pair token impersonated master")
	}
	// A new hub instance uses the persisted credential, including over public TLS.
	restarted := newTestHub(t)
	restarted.store = s
	mux := restarted.routes()
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", p.Token, "", "198.51.100.1:1", true), 200, "")
	requireCode(t, securityRequest(mux, "POST", "/api/v1/devices", p.Token, `{"device_id":"phone","name":"小王的手机"}`, "127.0.0.1:1", false), 200, "")
	// Old iOS connects WS before exchanging credentials, and may still use the token during grace.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/ws?token="+p.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	code, device := call(t, mux, "POST", "/api/v1/auth/tokens", p.Token, `{"name":"小王的手机","kind":"phone"}`)
	if code != 200 || device["role"] != roleFull {
		t.Fatalf("exchange %d %v", code, device)
	}
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", p.Token, "", "127.0.0.1:1", false), 200, "")
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "127.0.0.1:1", false), 401, "")
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/pair-codes", p.Token, "", "127.0.0.1:1", false), 403, "")
	s.mu.Lock()
	pt := s.pairs[hashToken(p.Token)]
	if pt.DeviceID != device["id"] || pt.UsedAt == nil {
		t.Fatal("redemption not recorded")
	}
	past := time.Now().Add(-pairGrace - time.Second)
	pt.UsedAt = &past
	s.pairs[pt.Hash] = pt
	s.mu.Unlock()
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", p.Token, "", "127.0.0.1:1", false), 401, "")
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", device["token"].(string), "", "198.51.100.1:1", true), 200, "")
}

func TestPairTokenExpiredAndOneUse(t *testing.T) {
	h := newTestHub(t)
	s := newPairStore()
	h.store = s
	p := newPairCode(t, h)
	s.mu.Lock()
	pt := s.pairs[hashToken(p.Token)]
	pt.ExpiresAt = time.Now().Add(-time.Second)
	s.pairs[pt.Hash] = pt
	s.mu.Unlock()
	for _, path := range []string{"/api/v1/machines", "/api/v1/ws", "/api/v1/auth/tokens"} {
		method := "GET"
		if strings.HasSuffix(path, "tokens") {
			method = "POST"
		}
		requireCode(t, securityRequest(h.routes(), method, path, p.Token, `{}`, "127.0.0.1:1", false), 401, "")
	}
	p = newPairCode(t, h)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	mux := h.routes()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "127.0.0.1:1", false).Code
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		} else if status != 401 {
			t.Fatalf("status %d", status)
		}
	}
	if success != 1 || len(s.rows) != 1 {
		t.Fatalf("issued %d credentials", success)
	}
}

func TestPairCodesRequireLoopbackAndMaster(t *testing.T) {
	h := newTestHub(t)
	h.store = newPairStore()
	h.cfg.Security.TrustedCIDRs = []string{"192.0.2.0/24"}
	_, device := call(t, h.routes(), "POST", "/api/v1/auth/tokens", h.cfg.Token, `{"kind":"phone"}`)
	for _, tc := range []struct {
		remote, token string
		tls           bool
	}{
		{"192.0.2.1:1", h.cfg.Token, false},
		{"198.51.100.1:1", h.cfg.Token, true},
		{"127.0.0.1:1", device["token"].(string), false},
	} {
		requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", tc.token, "", tc.remote, tc.tls), 403, "")
	}
	if len(h.store.(*pairStore).pairs) != 0 {
		t.Fatal("forbidden request created pairing code")
	}
	// IPv6 loopback is accepted too.
	requireCode(t, securityRequest(h.routes(), "POST", "/api/v1/auth/pair-codes", h.cfg.Token, "", "[::1]:1", false), 200, "")
}

func TestPairContextExpires(t *testing.T) {
	h := newTestHub(t)
	h.store = newPairStore()
	ctx, cancel := h.pairContext(context.Background(), principal{PairHash: "unknown", PairExpires: time.Now().Add(20 * time.Millisecond)})
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("pair WS context did not expire")
	}
}

func TestPairGraceDoesNotExtendExpiry(t *testing.T) {
	now := time.Now()
	used := now.Add(-pairGrace)
	p := &db.PairToken{ExpiresAt: now.Add(time.Hour), UsedAt: &used}
	if !pairExpires(p).Equal(now) {
		t.Fatal("redemption grace ignored")
	}
	p.ExpiresAt = now.Add(-time.Second)
	if !pairExpires(p).Equal(p.ExpiresAt) {
		t.Fatal("grace extended original expiry")
	}
}

func TestPairRepeatedRedemptionIsRateLimited(t *testing.T) {
	h := newTestHub(t)
	h.store = newPairStore()
	h.cfg.Security.AuthFailures = 1
	p := newPairCode(t, h)
	mux := h.routes()
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "198.51.100.1:1", true), 200, "")
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "198.51.100.1:1", true), 401, "")
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "198.51.100.1:1", true), 429, "rate_limited")
}

// Exercise the real migration and transaction through the HTTP flow, using only the lane's test database.
func TestPairTokenDatabaseFlow(t *testing.T) {
	dsn := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JARVIS_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := newTestHub(t)
	h.store = store
	p := newPairCode(t, h)
	if row, err := store.PairTokenByHash(ctx, hashToken(p.Token)); err != nil || row == nil || row.UsedAt != nil {
		t.Fatalf("pair row %v %v", row, err)
	}
	restarted := newTestHub(t)
	restarted.store = store
	mux := restarted.routes()
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", p.Token, "", "127.0.0.1:1", false), 200, "")
	code, device := call(t, mux, "POST", "/api/v1/auth/tokens", p.Token, `{"name":"小王的手机","kind":"phone"}`)
	if code != 200 {
		t.Fatalf("exchange %d %v", code, device)
	}
	requireCode(t, securityRequest(mux, "POST", "/api/v1/auth/tokens", p.Token, `{}`, "127.0.0.1:1", false), 401, "")
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", p.Token, "", "127.0.0.1:1", false), 200, "")
	requireCode(t, securityRequest(mux, "GET", "/api/v1/machines", device["token"].(string), "", "198.51.100.1:1", true), 200, "")
	_, _ = store.RevokeDeviceToken(ctx, device["id"].(string))
}
