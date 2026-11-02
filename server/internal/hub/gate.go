package hub

import "net/http"

// gate runs before authentication on every /api/ request: requests are refused here for how they arrive (plaintext
// from the internet, too many failed tokens) rather than for who sent them (docs/redesign/hub-changes.md 1.4).
func (h *Hub) gate(next http.Handler) http.Handler {
	if h.cfg.Security.AllowPublicPlaintext {
		h.logf("WARNING: security.allow_public_plaintext is enabled; public API traffic can expose credentials")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil && !h.cfg.Security.AllowPublicPlaintext && h.sourceOf(r) == sourcePublic {
			h.securityError(w, r, http.StatusForbidden, "tls_required", 0)
			return
		}
		next.ServeHTTP(w, r)
	})
}
