package hub

import (
	"context"
	"net/http"
	"slices"
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
	status, body = call(t, mux, "POST", "/api/v1/assistant/conversations/example/views/view_example/html-actions", token,
		`{"kind":"open","component_key":"chart","expected_version":1,"url":"https://example.com","client_msg_id":"example","confirmed":true}`)
	if status != http.StatusForbidden || body["code"] != "permission_denied" {
		t.Fatalf("extension html action: %d %v", status, body)
	}
}

// assistant.html_views defaults on, means nothing without persona_view, and reaches the assistant and hello.features.
func TestAssistantHTMLViewsSwitches(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		persona  bool
		override *bool
		html     *bool
		views    bool
		bridge   bool
	}{
		{false, nil, nil, false, false},
		{true, nil, nil, true, true},
		{true, nil, &off, true, false},
		{false, &on, nil, true, true},
		{true, &off, nil, false, false},
		{false, &on, &off, true, false},
	} {
		h := newTestHub(t)
		h.cfg.Assistant.PersonaView, h.cfg.Assistant.HTMLViews = tc.persona, tc.html
		h.admin.mu.Lock()
		h.admin.personaView = tc.override
		h.admin.mu.Unlock()
		hello, err := assistantLink{h}.Hello(context.Background())
		if err != nil || hello.PersonaView != tc.views || hello.HTMLViews != tc.bridge {
			t.Fatalf("%+v: hello %+v %v", tc, hello, err)
		}
		features := h.features()
		if slices.Contains(features, "assistant_views") != tc.views || slices.Contains(features, "assistant_html_bridge") != tc.bridge {
			t.Fatalf("%+v: features %v", tc, features)
		}
	}
}
