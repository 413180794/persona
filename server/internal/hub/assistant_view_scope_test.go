package hub

import (
	"net/http"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestAssistantCardActionsDenyExtensionTokens(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	token := h.extTokens.issue("example-extension", "Example", []string{extension.ScopeAssistantAsk, extension.ScopeAgentsRead, extension.ScopeAgentsControl})
	status, body := call(t, mux, "POST", "/api/v1/assistant/conversations/example/views/view_example/actions", token, `{"action":"yes","client_msg_id":"example"}`)
	if status != http.StatusForbidden || body["code"] != "permission_denied" {
		t.Fatalf("extension card action: %d %v", status, body)
	}
}
