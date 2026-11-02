package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/github"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
)

// The MCP tools the hub offers, as they go over the wire (json.Marshal compacts the schemas; the golden is indented
// for reading, which is a function of the same bytes). docs/prompts.md: regenerate with UPDATE_GOLDEN=1.
func TestGoldenMCPTools(t *testing.T) {
	show := func(v any) string {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return string(b) + "\n"
	}
	h := newTestHub(t)
	h.mail = (*mail.Service)(nil) // Tools() reads nothing from the service
	golden(t, "mcp_tools.assistant_without_lark", show(h.mcpTools(true)))
	h.lark = lark.New(lark.Config{BaseURL: "http://127.0.0.1:1", Token: "t"})
	golden(t, "mcp_tools.assistant", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
	// With GitHub signed in: its read tools after the hub's own, for the assistant only.
	h.connectors = connector.NewRegistry(nil)
	if err := h.addConnector(signedInGitHub{github.New(github.Config{}, "Mac", nil)}); err != nil {
		t.Fatal(err)
	}
	golden(t, "mcp_tools.assistant_with_github", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
}

type signedInGitHub struct{ *github.Connector }

func (signedInGitHub) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK}
}
