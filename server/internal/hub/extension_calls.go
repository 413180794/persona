package hub

import (
	"context"
	"net/http"
	"sync"
)

// What a device has open through the manager (docs/extensions.md「转发时的调用方」): every request the hub relays to
// the manager (relayToManager), an extension's WebSocket or stream above all, is kept under the id of the credential
// that made it, so revoking that credential ends them at once instead of when the phone hangs up (a voice call, #3498).
// Ending the hub's side is enough: the relay stream closes and the manager's proxy closes the extension's side.

// relayedCalls are the requests being relayed to the manager now, by credential id.
type relayedCalls struct {
	mu   sync.Mutex
	byID map[string]map[*context.CancelFunc]struct{}
}

// trackRelayed keeps r, about to be relayed, under its caller's credential so that revoking it ends the call
// (dropRelayed): relay the request it returns, then call done. The master token's and an admin session's are not kept:
// neither is revoked. A pairing code's call also ends when the code does (its expiry, or the redemption grace once a
// device redeemed it, which revoking that device ends), as the code's /api/v1/ws does (pairContext).
func (h *Hub) trackRelayed(r *http.Request) (_ *http.Request, done func()) {
	p := principalOf(r.Context())
	id := p.ID
	if id == "" {
		return r, func() {}
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if p.PairHash != "" {
		ctx, cancel = h.pairContext(r.Context(), p)
	} else {
		ctx, cancel = context.WithCancel(r.Context())
	}
	r = r.WithContext(ctx)
	calls := &h.ext.calls
	key := &cancel
	calls.mu.Lock()
	if calls.byID == nil {
		calls.byID = map[string]map[*context.CancelFunc]struct{}{}
	}
	if calls.byID[id] == nil {
		calls.byID[id] = map[*context.CancelFunc]struct{}{}
	}
	calls.byID[id][key] = struct{}{}
	calls.mu.Unlock()
	// Revoked between the auth and here: revoked has already ended what was kept, so this one ends by itself.
	if h.isDeviceRevoked(id) {
		cancel()
	}
	return r, func() {
		calls.mu.Lock()
		if m := calls.byID[id]; m != nil {
			delete(m, key)
			if len(m) == 0 {
				delete(calls.byID, id)
			}
		}
		calls.mu.Unlock()
		cancel()
	}
}

// dropRelayed ends what credential id has relayed to the manager now (it was revoked): the request's context ending
// closes both sides of a relayed WebSocket (httputil.ReverseProxy). The number ended.
func (h *Hub) dropRelayed(id string) int {
	if id == "" {
		return 0
	}
	calls := &h.ext.calls
	calls.mu.Lock()
	var cancels []context.CancelFunc
	for k := range calls.byID[id] {
		cancels = append(cancels, *k)
	}
	delete(calls.byID, id)
	calls.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return len(cancels)
}
