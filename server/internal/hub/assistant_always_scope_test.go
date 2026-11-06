package hub

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestAssistantAlwaysGrantsDenyExtensionTokens(t *testing.T) {
	h := newTestHub(t)
	mux := h.routes()
	token := extTokensOf(t, h).issue("example-extension", "Example", []string{extension.ScopeAssistantAsk, extension.ScopeAgentsRead, extension.ScopeAgentsControl})
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/assistant/conversations/example/always"},
		{"DELETE", "/api/v1/assistant/conversations/example/always/" + strings.Repeat("a", 64)},
	} {
		status, body := call(t, mux, tc.method, tc.path, token, "")
		if status != http.StatusForbidden || body["code"] != "permission_denied" {
			t.Fatalf("%s: %d %v", tc.method, status, body)
		}
	}
}
