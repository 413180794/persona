package hub

import (
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

// An extension's token on a route the hub relays to the manager is checked on the path the request is relayed with:
// the escaped one. Checked on the decoded path, an escaped dot segment is cleaned away and the manager checks another
// route than the one it then serves (#5182).
func TestExtensionAuthorizeSeesTheRelayedPath(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	f := extTokensOf(t, h)
	token := f.issue("example-extension", "Example", []string{extension.ScopeAgentsRead})
	for _, path := range []string{
		"/api/v1/extensions/other/%2E%2E/%2E%2E/agents",
		"/api/v1/extensions/other/x%2F..%2F..%2F..%2Fagents",
		"/api/v1/extensions/other/page",
	} {
		f.mu.Lock()
		f.asked = nil
		f.mu.Unlock()
		call(t, mux, "GET", path, token, "")
		f.mu.Lock()
		asked := slices.Clone(f.asked)
		f.mu.Unlock()
		if len(asked) == 0 {
			t.Fatalf("%s: the manager was not asked", path)
		}
		if got := asked[len(asked)-1]; got.Path != path || got.Pattern != "/api/v1/extensions/" {
			t.Fatalf("%s: asked about %q on %q, want the path the request is relayed with", path, got.Path, got.Pattern)
		}
	}
}
