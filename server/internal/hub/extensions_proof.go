package hub

// An extension's token from the internet is taken only with its manager's proof that the request came through that
// manager (extwire.HeaderManagerProof, docs/managers.md "Extensions and the hub's API"): a token that leaked is no use
// on its own. The key is made from the token of the manager's live bus connection as it connects (extwire.ProofKey)
// and kept with that connection only, so a manager revoked, replaced or gone proves nothing, and the hub's database
// (which has another hash of the token) forges nothing. Each proof is taken once, within extwire.ProofWindow of the
// hub's clock. The nonces are in memory: a hub that restarts forgets them, so a proof captured in its last
// ProofWindow could be taken once more after it (capturing one means breaking the TLS it came over).

import (
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/extwire"
)

// The answers to a manager's proof that is its own but not taken (docs/managers.md "Extensions and the hub's API").
const (
	codeClockSkew     = extwire.CodeClockSkew
	codeProofReplayed = extwire.CodeProofReplayed
	codeProofBusy     = extwire.CodeProofBusy
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
