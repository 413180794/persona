package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
)

// tokenStore keeps device tokens in memory, like the device_tokens table.
type tokenStore struct {
	memStore
	mu   sync.Mutex
	rows map[string]db.DeviceToken // hash -> token
	rev  map[string]bool
}

func (s *tokenStore) CreateDeviceToken(_ context.Context, t db.DeviceToken, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.CreatedAt = time.Now()
	s.rows[hash] = t
	return nil
}

func (s *tokenStore) DeviceTokenByHash(_ context.Context, hash string) (*db.DeviceToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.rows[hash]
	if !ok || s.rev[t.ID] {
		return nil, nil
	}
	return &t, nil
}

func (s *tokenStore) DeviceTokens(context.Context) ([]db.DeviceToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []db.DeviceToken
	for _, t := range s.rows {
		out = append(out, t)
	}
	return out, nil
}

func (s *tokenStore) RevokeDeviceToken(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.rows {
		if t.ID == id && !s.rev[id] {
			s.rev[id] = true
			return true, nil
		}
	}
	return false, nil
}

func (s *tokenStore) TouchDeviceToken(context.Context, string) error { return nil }

func call(t *testing.T, h http.Handler, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestDeviceTokens(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	mux := h.routes()
	master := h.cfg.Token

	code, phone := call(t, mux, "POST", "/api/v1/auth/tokens", master, `{"name":"iPhone","kind":"phone"}`)
	if code != 200 || phone["role"] != "full" || !strings.HasPrefix(phone["token"].(string), "jd_") {
		t.Fatalf("%d %v", code, phone)
	}
	phoneTok := phone["token"].(string)
	code, watch := call(t, mux, "POST", "/api/v1/auth/tokens", phoneTok, `{"name":"Watch","kind":"watch"}`)
	if code != 200 || watch["role"] != "answer" {
		t.Fatalf("the phone adds its watch: %d %v", code, watch)
	}
	watchTok := watch["token"].(string)

	// The watch reads and answers, nothing else.
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", watchTok, ""); code != 200 {
		t.Fatalf("watch read %d", code)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/interactions/ix_x/resolve", watchTok, `{"allow":true}`); code != http.StatusGone {
		t.Fatalf("watch answer reaches the handler (410: no such card), got %d", code)
	}
	for _, p := range []string{"/api/v1/auth/tokens", "/api/v1/machines/m/reconnect", "/api/v1/sessions"} {
		if code, _ := call(t, mux, "POST", p, watchTok, `{}`); code != http.StatusForbidden {
			t.Fatalf("watch POST %s: %d", p, code)
		}
	}

	// Revoking the phone shuts it out at once; the pairing token and the watch keep working.
	if code, _ := call(t, mux, "DELETE", "/api/v1/auth/tokens/"+phone["id"].(string), master, ""); code != 200 {
		t.Fatal(code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", phoneTok, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked phone: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", watchTok, ""); code != 200 {
		t.Fatalf("watch after the phone's revocation: %d", code)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/interactions", "jd_made_up", ""); code != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", code)
	}
	code, list := call(t, mux, "GET", "/api/v1/auth/tokens", watchTok, "")
	if code != 200 || len(list["tokens"].([]any)) != 2 || list["current"] != watch["id"] {
		t.Fatalf("%d %v", code, list)
	}
}
