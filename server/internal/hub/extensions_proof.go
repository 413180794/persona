package hub

// An extension's token from the internet is taken only with its manager's proof that the request came through that
// manager (extwire.HeaderManagerProof, docs/assistant/managers.md "Extensions and the hub's API"): a token that leaked is no use
// on its own. The key is made from the token of the manager's live bus connection as it connects (extwire.ProofKey)
// and kept with that connection only, so a manager revoked, replaced or gone proves nothing, and the hub's database
// (which has another hash of the token) forges nothing. Each proof is taken once, within extwire.ProofWindow of the
// hub's clock. The nonces are in memory: a hub that restarts forgets them, so a proof captured in its last
// ProofWindow could be taken once more after it (capturing one means breaking the TLS it came over).

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extwire"
)

// codeAuthorizeUnsure (503): a manager did not answer whose an extension's token is (it timed out, or is not ready
// yet), so there is no verdict on the token: not counted, and not marked as the token being nobody's.
const codeAuthorizeUnsure = "manager_unreachable"

// The answers to a manager's proof that is its own but not taken (docs/assistant/managers.md "Extensions and the hub's API").
const (
	codeClockSkew     = extwire.CodeClockSkew
	codeProofReplayed = extwire.CodeProofReplayed
	codeProofBusy     = extwire.CodeProofBusy
	// codeManagerOffline: a proof that names a manager of the roster with no live bus connection (proofOfAManagerAway).
	codeManagerOffline = extwire.CodeManagerOffline
)

// proofNoncesMost bounds the nonces kept of each manager: past it that manager's proofs are refused (proofBusy) rather
// than a nonce forgotten early; the other managers' are not affected.
const proofNoncesMost = 1 << 16

// proofNonces are the proofs' nonces seen, by manager, each until its proof could no longer be fresh.
type proofNonces struct {
	mu    sync.Mutex
	byMgr map[string]*managerNonces
	swept time.Time
}

type managerNonces struct {
	seen map[string]time.Time // nonce → when it may be forgotten
}

// proofVerdict is what a request's proof is worth.
type proofVerdict int

const (
	proofOK proofVerdict = iota
	// proofBad: none, not one of the token's manager's, or not signed with its live key for this request and token.
	// The token alone from the internet: refused and counted against the source, as a bad token.
	proofBad
	// proofSkew: signed by the manager, at a time out of the window (its clock or the hub's is off). Refused, not
	// counted: the source is the manager's, which did nothing wrong but keep a clock.
	proofSkew
	// proofReplayed: signed by the manager, but its nonce was taken already (a request sent twice). Refused, not
	// counted.
	proofReplayed
	// proofBusy: the manager's nonces are at proofNoncesMost. Refused for a while, not counted.
	proofBusy
)

// first records manager's nonce of a proof made at made, and says whether it was not seen before (proofOK),
// seen (proofReplayed), or the manager has too many now (proofBusy).
func (n *proofNonces) first(manager, nonce string, made, now time.Time) proofVerdict {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.byMgr == nil {
		n.byMgr = map[string]*managerNonces{}
	}
	if now.Sub(n.swept) >= time.Second {
		n.swept = now
		for id, m := range n.byMgr {
			for k, until := range m.seen {
				if !now.Before(until) {
					delete(m.seen, k)
				}
			}
			if len(m.seen) == 0 {
				delete(n.byMgr, id)
			}
		}
	}
	m := n.byMgr[manager]
	if m == nil {
		m = &managerNonces{seen: map[string]time.Time{}}
		n.byMgr[manager] = m
	}
	if until, ok := m.seen[nonce]; ok && now.Before(until) {
		return proofReplayed
	}
	if len(m.seen) >= proofNoncesMost {
		return proofBusy
	}
	m.seen[nonce] = made.Add(extwire.ProofWindow + time.Second)
	return proofOK
}

// busProofKey is the key of manager's proofs, of its live bus connection ("" when it has none).
func (h *Hub) busProofKey(manager string) string {
	h.bus.mu.Lock()
	defer h.bus.mu.Unlock()
	if c := h.bus.current[manager]; c != nil && !c.ended && !c.replaced {
		return c.proofKey
	}
	return ""
}

// proofOfAManagerAway says whether proof (a request's extwire.HeaderManagerProof, as it came) names a manager of the
// roster that has no live bus connection now. Only its shape is read: without that connection there is no key to check
// it with. A proof naming no manager of the roster, or one that is connected, is not this (it is checked, and counted
// when it fails, as before).
func (h *Hub) proofOfAManagerAway(ctx context.Context, proof string) bool {
	if proof == "" {
		return false
	}
	p, err := extwire.ParseProof(proof)
	if err != nil || h.busProofKey(p.Manager) != "" {
		return false
	}
	// Only while the hub may have lost it unawares: an answer that is not counted tells whoever asks that the manager
	// is in the roster and off the bus, so outside that window it is counted as ever.
	now := time.Now()
	if !h.extLost.recently(p.Manager, now) {
		return false
	}
	if p.Manager == assistantwire.AssistantID || h.busConnOf(p.Manager) != nil {
		return true // the roster's own shortcut (rosterHas)
	}
	// Such a proof is not counted, so the roster is asked once in a while for it, not on each request.
	if has, ok := h.extRoster.get(p.Manager, now); ok {
		return has
	}
	has, sure := h.rosterHas(ctx, p.Manager)
	if sure {
		h.extRoster.put(p.Manager, has, now)
	}
	return has && sure
}

// lostWindow is how long after this hub started, or after a manager's bus connection ended here, a proof naming that
// manager off the bus is answered 503 without counting (proofOfAManagerAway): about how long a manager takes to notice
// a hub that restarted, or a connection gone half open, and come back. Past it the proof is counted as any other.
const lostWindow = 2 * time.Minute

// busLosses are when this hub started, and when each manager's bus connection ended here lately.
type busLosses struct {
	mu      sync.Mutex
	started time.Time            // zero: not known (tests)
	ended   map[string]time.Time // manager id → when its current connection ended
}

// lost records that manager id's current bus connection ended at now.
func (l *busLosses) lost(id string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ended == nil {
		l.ended = map[string]time.Time{}
	}
	for k, at := range l.ended {
		if now.Sub(at) >= lostWindow {
			delete(l.ended, k)
		}
	}
	l.ended[id] = now
}

// recently says whether now is within lostWindow of this hub's start or of the end of id's last connection here.
func (l *busLosses) recently(id string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.started.IsZero() && now.Sub(l.started) < lostWindow {
		return true
	}
	at, ok := l.ended[id]
	return ok && now.Sub(at) < lostWindow
}

// proofVouches says whether proof is a fresh one the manager it names signed with the key of its live bus connection
// for r with tok: whoever sent r came through that manager's relay. Its nonce is not taken (the request is not let
// through on it).
func (h *Hub) proofVouches(r *http.Request, proof, tok string) bool {
	p, err := extwire.ParseProof(proof)
	if proof == "" || err != nil {
		return false
	}
	uri := r.RequestURI
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	return p.Valid(h.busProofKey(p.Manager), r.Method, uri, tok) && p.Fresh(time.Now())
}

// rosterSeenFor is how long rosterSeen keeps an answer, and rosterSeenMost how many it keeps.
const (
	rosterSeenFor  = 5 * time.Second
	rosterSeenMost = 1024
)

// rosterSeen are the roster's answers lately to whether a manager off the bus is in it (proofOfAManagerAway).
type rosterSeen struct {
	mu   sync.Mutex
	seen map[string]rosterAnswer
}

type rosterAnswer struct {
	has   bool
	until time.Time
}

func (c *rosterSeen) get(id string, now time.Time) (has, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.seen[id]
	if !ok || !now.Before(a.until) {
		return false, false
	}
	return a.has, true
}

// forget drops what was kept of id (it left the roster).
func (c *rosterSeen) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.seen, id)
}

func (c *rosterSeen) put(id string, has bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil || len(c.seen) >= rosterSeenMost {
		c.seen = map[string]rosterAnswer{} // (each answer is good for seconds: starting over loses nothing)
	}
	c.seen[id] = rosterAnswer{has: has, until: now.Add(rosterSeenFor)}
}

// extensionProof is what proof (r's extwire.HeaderManagerProof) is worth for r with the extension's token tok of
// manager: signed with the key of manager's live bus connection for r's method, URI and tok, then fresh, then its
// nonce not seen before.
func (h *Hub) extensionProof(r *http.Request, proof, tok, manager string) proofVerdict {
	if proof == "" || manager == "" {
		return proofBad
	}
	p, err := extwire.ParseProof(proof)
	if err != nil || p.Manager != manager {
		return proofBad
	}
	uri := r.RequestURI // as it was sent, which the manager signed
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	if !p.Valid(h.busProofKey(manager), r.Method, uri, tok) {
		return proofBad
	}
	now := time.Now()
	if !p.Fresh(now) {
		return proofSkew
	}
	return h.extProofs.first(manager, p.Nonce, p.Time, now)
}
