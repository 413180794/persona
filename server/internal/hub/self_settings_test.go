package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestSelfSettingsRedactsSecretsWithoutChangingDelivery(t *testing.T) {
	// The history's redaction is the manager's (assistant TestRedacted); the push offers no quick approval.
	payload := interactionPayload(proto.Interaction{Kind: proto.IxPermission, Fields: []proto.Field{{Key: "password", Secret: true}}}, "server")
	if payload["aps"].(map[string]any)["category"] != "INTERACTION_INPUT" {
		t.Fatal("notification offers quick approval")
	}
}

func TestSelfSettingsToolOnlyForAssistant(t *testing.T) {
	h := newTestHub(t)
	connectAssistant(t, h)
	for _, tool := range h.mcpTools(false) {
		if tool.Name == "persona_settings" {
			t.Fatal("ordinary agent gets self settings")
		}
	}
	found := false
	for _, tool := range h.mcpTools(true) {
		found = found || tool.Name == "persona_settings"
	}
	if !found || !h.isAssistantTool(context.Background(), "persona_settings") {
		t.Fatal("assistant lacks self settings")
	}
	// The list is the assistant's: its own settings and the hub's (settings.list).
	res := h.asst().Tool(context.Background(), assistantwire.ToolCall{Name: "persona_settings", Arguments: json.RawMessage(`{}`)})
	out := res.Content[0].Text
	// The inbox's rules and the mail accounts are the manager's settings (extmgr), not connected here.
	if res.IsError || !strings.Contains(out, "- assistant.notes (关于我)") || strings.Contains(out, "- mail.accounts") || strings.Contains(out, "- inbox.rules ") {
		t.Fatalf("list: %q", out)
	}
}
