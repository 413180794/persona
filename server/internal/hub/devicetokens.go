package hub

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/link"
	"github.com/nwverse/persona/server/internal/proto"
)

// One credential per device (docs/protocol.md「鉴权」). The pairing token in hub.json still opens everything (the CLI,
// apps from before this); an app trades it for a token of its own right after pairing and stops keeping it, so a lost
// phone is revoked alone instead of re-pairing every device. A watch gets an answer-only token: it can do what the
// watch app does (answerReads, answerWrites) and nothing else.

const (
	roleFull   = "full"
	roleAnswer = "answer"
	// roleLink is a jarvis-link credential (link.go): it opens its machine's link and nothing else.
	roleLink = "link"
	// roleLinkPair is a pairing code made for one machine's jarvis-link (`jarvisd hub pair --machine`): all it can
	// do is become that credential.
	roleLinkPair = "link_pair"
)

// principal is who a request comes from. Only the master token has an empty ID.
type principal struct {
	ID          string
	Name        string
	Role        string
	PairHash    string
	PairExpires time.Time
	Machine     string // roleLink, roleLinkPair: the machine in hub.json it is bound to; rolePeer: the peer's hub_id
}

type principalKey struct{}

func principalOf(ctx context.Context) principal {
	if p, ok := ctx.Value(principalKey{}).(principal); ok {
		return p
	}
	return principal{Role: roleFull} // handlers called directly (tests)
}

// byOwnDevice says whether the request came from one of the user's own signed-in apps (phone, Mac, web): a full device
// token, as the hub's auth found it, never a field the client set. What such a request writes the user sent by hand,
// so it is not put to the write gate, which is for the AI's (connector.Registry.Run). The pairing token in hub.json,
// a pairing code and machine or mesh credentials are not a device: they stay behind the gate.
func byOwnDevice(ctx context.Context) bool {
	p, ok := ctx.Value(principalKey{}).(principal)
	return ok && p.Role == roleFull && p.ID != "" && p.PairHash == ""
}

// devTokens caches token lookups so an app's burst of requests is one query.
type devTokens struct {
	mu       sync.Mutex
	cache    map[string]cachedToken // token hash -> lookup
	touched  map[string]time.Time   // device id -> last_seen written
	failures authLimiter
	vpnLocal func(netip.Addr) bool // nil uses the OS interface list
	// onLink lists the IPv6 prefixes on this machine's interfaces (ingress.go); nil reads the OS interface list.
	onLink     func() []netip.Prefix
	linkCache  []netip.Prefix
	linkCached time.Time
}

type cachedToken struct {
	p    *principal // nil: no such token
	at   time.Time
	used time.Time
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
	if strings.HasPrefix(tok, "jx_") { // a code pairing another hub (peer_pair.go)
		return h.peerPairPrincipal(ctx, tok)
	}
	hash := hashToken(tok)
	if strings.HasPrefix(tok, "jp_") {
		pt, err := h.store.PairTokenByHash(ctx, hash)
		if err != nil || pt == nil || !time.Now().Before(pairExpires(pt)) {
			return principal{}, false
		}
		role := roleFull
		if pt.MachineID != "" {
			role = roleLinkPair
		}
		return principal{ID: "pair_" + hash, Role: role, PairHash: hash, PairExpires: pairExpires(pt), Machine: pt.MachineID}, true
	}
	t := &h.devtok
	t.mu.Lock()
	c, hit := t.cache[hash]
	if hit {
		c.used = time.Now()
		t.cache[hash] = c
	}
	t.mu.Unlock()
	if !hit || time.Since(c.at) > tokenCacheFor {
		dt, err := h.store.DeviceTokenByHash(ctx, hash)
		if err != nil {
			return principal{}, false
		}
		c = cachedToken{at: time.Now(), used: time.Now()}
		if dt != nil {
			c.p = &principal{ID: dt.ID, Name: dt.Name, Role: dt.Role, Machine: dt.MachineID}
		}
		t.mu.Lock()
		if t.cache == nil {
			t.cache, t.touched = map[string]cachedToken{}, map[string]time.Time{}
		}
		if len(t.cache) >= h.cfg.Security.cacheEntries() {
			oldestKey := ""
			var oldest time.Time
			for key, entry := range t.cache {
				if time.Since(entry.at) > tokenCacheFor {
					delete(t.cache, key)
				} else if oldestKey == "" || entry.used.Before(oldest) {
					oldestKey, oldest = key, entry.used
				}
			}
			if len(t.cache) >= h.cfg.Security.cacheEntries() {
				delete(t.cache, oldestKey)
			}
		}
		t.cache[hash] = c
		t.mu.Unlock()
	}
	if c.p == nil {
		return principal{}, false
	}
	t.mu.Lock()
	if t.touched == nil {
		t.touched = map[string]time.Time{}
	}
	if len(t.touched) >= h.cfg.Security.cacheEntries() {
		for id, at := range t.touched {
			if time.Since(at) >= 5*time.Minute {
				delete(t.touched, id)
			}
		}
		if len(t.touched) >= h.cfg.Security.cacheEntries() {
			for id := range t.touched {
				delete(t.touched, id)
				break
			}
		}
	}
	touch := time.Since(t.touched[c.p.ID]) > 5*time.Minute
	if touch {
		t.touched[c.p.ID] = time.Now()
	}
	t.mu.Unlock()
	if touch {
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.store.TouchDeviceToken(ctx, id)
		}(c.p.ID)
	}
	return *c.p, true
}

// answerReads and answerWrites are all an answer-only device may do: what the watch app does and has done, no more. A
// GET is not harmless for being a GET (one reads any file of a computer, one runs a launcher), so none gets through for
// its method. Something new the watch needs goes here.
var (
	answerReads  = regexp.MustCompile(`^/api/v1/(machines|sessions|interactions|inbox|sessions/[^/]+/[^/]+/items)$`)
	answerWrites = regexp.MustCompile(`^/api/v1/(interactions/[^/]+/resolve|sessions/[^/]+/[^/]+/(messages|seen|interrupt)|` +
		`inbox/[^/]+/(done|reply|draft))$`)
	// answerKey is a key the watch can press on a screen card it could not read: a menu's number, Esc, Return.
	answerKey = regexp.MustCompile(`^([1-9]|esc|enter)$`)
)

func allowed(p principal, method, path string) bool {
	switch p.Role {
	case roleFull:
		return true
	case roleLink:
		return method == http.MethodGet && (path == link.Path || path == link.StreamPath)
	case roleLinkPair:
		return method == http.MethodPost && path == "/api/v1/auth/tokens"
	case rolePeer:
		return method == http.MethodGet && path == peerPath
	case rolePeerPair:
		return method == http.MethodPost && path == peerPairPath
	case roleAnswer:
		if method == http.MethodGet || method == http.MethodHead {
			return answerReads.MatchString(path)
		}
		return method == http.MethodPost && answerWrites.MatchString(path)
	}
	return false
}

// answerMayResolve is what an answer-only device may say to a card: pick an answer, say yes or no and why. Typing into a
// terminal, pressing more than a menu's key, filling in a card's fields and making a tool's approval stand (always) are
// for a full device. A screen card's own "don't ask again" is one of its options, and the watch shows it.
func answerMayResolve(r proto.Resolution) bool {
	if r.Text != "" || len(r.PreKeys) > 0 || r.Always || r.Message != "" || len(r.Values) > 0 || r.Action != "" || len(r.Content) > 0 {
		return false
	}
	return len(r.Keys) == 0 || len(r.Keys) == 1 && answerKey.MatchString(r.Keys[0])
}

func (h *Hub) tokenRoutes(api *http.ServeMux) {
	// Trade the caller's credential for a device token. kind "watch" gets an answer-only one; kind "link" a
	// jarvis-link credential for the link machine named in "machine" (link.go).
	api.HandleFunc("POST /api/v1/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Machine string `json:"machine"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		p := principalOf(r.Context())
		if p.Role != roleFull && p.Role != roleLinkPair {
			writeErr(w, http.StatusForbidden, errors.New("this device may not add devices"))
			return
		}
		if h.store == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("no database"))
			return
		}
		kind, role, machine := req.Kind, roleFull, ""
		if p.Role == roleLinkPair { // made for one machine's jarvis-link: that credential, and no other
			if req.Machine != "" && req.Machine != p.Machine {
				writeErr(w, http.StatusForbidden, fmt.Errorf("this pairing code is for machine %q", p.Machine))
				return
			}
			kind, req.Machine = "link", p.Machine
		}
		switch kind {
		case "link":
			machine, role = req.Machine, roleLink
			if m := h.cfgMachine(machine); m == nil || m.cfg.Kind != kindLink { // the hub.json entry (link.go)
				writeErr(w, http.StatusBadRequest, fmt.Errorf("hub.json has no machine %q of kind link", machine))
				return
			}
		case "watch":
			role = roleAnswer
		case "phone":
		default:
			kind = "other"
		}
		name := strings.TrimSpace(req.Name)
		if name == "" || len([]rune(name)) > 80 {
			name = map[string]string{"phone": "iPhone", "watch": "Apple Watch"}[kind]
			if kind == "link" {
				name = h.cfgMachine(machine).cfg.Name + " 的 Persona Link"
			}
			if name == "" {
				name = "设备"
			}
		}
		tok := "jd_" + randomHex(24)
		dt := db.DeviceToken{ID: "dev_" + randomHex(6), Name: name, Kind: kind, Role: role, MachineID: machine}
		var err error
		if p.PairHash != "" {
			var ok bool
			ok, err = h.store.RedeemPairToken(r.Context(), p.PairHash, dt, hashToken(tok))
			if err == nil && !ok {
				failures := 0
				if h.sourceOf(r) == sourcePublic {
					failures = h.devtok.failures.fail(authSourceKey(r), time.Now(), h.cfg.Security)
				}
				h.securityError(w, r, http.StatusUnauthorized, "", failures)
				return
			}
		} else {
			err = h.store.CreateDeviceToken(r.Context(), dt, hashToken(tok))
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		h.logf("device token %s for %q (%s, %s)", dt.ID, name, kind, role)
		h.notifyPairing(name)
		writeJSON(w, map[string]any{"id": dt.ID, "token": tok, "role": role, "name": name, "machine": machine})
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
		h.sendRevocations(newlyReachable(nil, h.meshView()))
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
	for deviceID, r := range h.phoneTools {
		if deviceID == id || r.CredentialID == id {
			delete(h.phoneTools, deviceID)
			delete(h.wokeAt, deviceID)
		}
	}
	var gone []*client
	for c := range h.clients {
		if c.device == id {
			c.phone = false
			gone = append(gone, c)
		}
	}
	h.mu.Unlock()
	for _, c := range gone {
		c.conn.CloseNow()
	}
	links := h.closeLinks(id) + h.closePeerLinksOf(id)
	if hub := h.peerForCredential(id); hub != "" { // a peer's credential: that pairing ends
		go func() {
			if _, err := h.unpairPeer(context.Background(), hub, true); err != nil {
				h.logf("mesh: unpair after revoking %s: %v", id, err)
			}
		}()
	}
	h.push.signedOut(id) // nil-safe; forgets the devices this credential registered (push.go)
	h.logf("device token %s revoked (%d connections closed)", id, len(gone)+links)
}
