package hub

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/db"
)

// One credential per device (docs/protocol.md「鉴权」). The pairing token in hub.json still opens everything (the CLI,
// apps from before this); an app trades it for a token of its own right after pairing and stops keeping it, so a lost
// phone is revoked alone instead of re-pairing every device. A watch gets an answer-only token: it can read, answer,
// reply and mark done, not change settings, start or close agents, or run tools.

const (
	roleFull   = "full"
	roleAnswer = "answer"
)

// principal is who a request comes from: the pairing token (no id) or a device token.
type principal struct {
	ID   string
	Name string
	Role string
}

type principalKey struct{}

func principalOf(ctx context.Context) principal {
	if p, ok := ctx.Value(principalKey{}).(principal); ok {
		return p
	}
	return principal{Role: roleFull} // handlers called directly (tests)
}

// devTokens caches token lookups so an app's burst of requests is one query.
type devTokens struct {
	mu      sync.Mutex
	cache   map[string]cachedToken // token hash -> lookup
	touched map[string]time.Time   // device id -> last_seen written
}

type cachedToken struct {
	p  *principal // nil: no such token
	at time.Time
}

const tokenCacheFor = 30 * time.Second

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// authenticate maps a bearer token to its principal; ok false for an unknown or revoked one.
func (h *Hub) authenticate(ctx context.Context, tok string) (principal, bool) {
	if tok == "" {
		return principal{}, false
	}
	if subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.Token)) == 1 {
		return principal{Role: roleFull}, true
	}
	if h.store == nil {
		return principal{}, false
	}
	hash := hashToken(tok)
	t := &h.devtok
	t.mu.Lock()
	c, hit := t.cache[hash]
	t.mu.Unlock()
	if !hit || time.Since(c.at) > tokenCacheFor {
		dt, err := h.store.DeviceTokenByHash(ctx, hash)
		if err != nil {
			return principal{}, false
		}
		c = cachedToken{at: time.Now()}
		if dt != nil {
			c.p = &principal{ID: dt.ID, Name: dt.Name, Role: dt.Role}
		}
		t.mu.Lock()
		if t.cache == nil {
			t.cache, t.touched = map[string]cachedToken{}, map[string]time.Time{}
		}
		t.cache[hash] = c
		t.mu.Unlock()
	}
	if c.p == nil {
		return principal{}, false
	}
	t.mu.Lock()
	touch := time.Since(t.touched[c.p.ID]) > 5*time.Minute
	if touch {
		t.touched[c.p.ID] = time.Now()
	}
	t.mu.Unlock()
	if touch {
		go func(id string) { _ = h.store.TouchDeviceToken(context.Background(), id) }(c.p.ID)
	}
	return *c.p, true
}

// answerWrites are what an answer-only device may change: answering, sending and replying, seen / done, its own
// push registration. Everything else that is not a read is refused.
var answerWrites = regexp.MustCompile(`^/api/v1/(interactions/[^/]+/resolve|sessions/[^/]+/[^/]+/(messages|seen|interrupt)|` +
	`inbox/[^/]+/(done|reply|draft)|inbox/refresh|devices(/.*)?)$`)

func allowed(p principal, method, path string) bool {
	if p.Role == roleFull {
		return true
	}
	if method == http.MethodGet || method == http.MethodHead {
		return true
	}
	return p.Role == roleAnswer && answerWrites.MatchString(path)
}

func (h *Hub) tokenRoutes(api *http.ServeMux) {
	// Trade the caller's credential for a device token. kind "watch" gets an answer-only one.
	api.HandleFunc("POST /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device may not add devices"))
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		kind, role := req.Kind, roleFull
		switch kind {
		case "watch":
			role = roleAnswer
		case "phone":
		default:
			kind = "other"
		}
		name := strings.TrimSpace(req.Name)
		if name == "" || len([]rune(name)) > 80 {
			name = map[string]string{"phone": "iPhone", "watch": "Apple Watch"}[kind]
			if name == "" {
				name = "设备"
			}
		}
		tok := "jd_" + randomHex(24)
		dt := db.DeviceToken{ID: "dev_" + randomHex(6), Name: name, Kind: kind, Role: role}
		if err := h.store.CreateDeviceToken(r.Context(), dt, hashToken(tok)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("device token %s for %q (%s, %s)", dt.ID, name, kind, role)
		writeJSON(w, map[string]any{"id": dt.ID, "token": tok, "role": role, "name": name})
	})
	api.HandleFunc("GET /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		list := []db.DeviceToken{}
		if h.store != nil {
			var err error
			if list, err = h.store.DeviceTokens(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		writeJSON(w, map[string]any{"tokens": list, "current": principalOf(r.Context()).ID})
	})
	api.HandleFunc("DELETE /api/v1/auth/tokens/{id}", func(w http.ResponseWriter, r *http.Request) {
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		id := r.PathValue("id")
		ok, err := h.store.RevokeDeviceToken(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, errors.New("no such device"))
			return
		}
		h.revoked(id)
		writeJSON(w, map[string]any{"ok": true})
	})
}

// revoked forgets a device's cached lookups, closes its live connections and stops its pushes.
func (h *Hub) revoked(id string) {
	t := &h.devtok
	t.mu.Lock()
	for k, c := range t.cache {
		if c.p != nil && c.p.ID == id {
			delete(t.cache, k)
		}
	}
	t.mu.Unlock()
	h.mu.Lock()
	var gone []*client
	for c := range h.clients {
		if c.device == id {
			gone = append(gone, c)
		}
	}
	h.mu.Unlock()
	for _, c := range gone {
		c.conn.CloseNow()
	}
	h.push.signedOut(id)
	h.logf("device token %s revoked (%d connections closed)", id, len(gone))
}
