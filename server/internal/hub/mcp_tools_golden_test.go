package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/mail"
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
	connectAssistant(t, h)        // its tools are its to list (assistant.tools)
	h.mail = (*mail.Service)(nil) // Tools() reads nothing from the service
	register := func() {          // the connectors as the hub registers them (mail's and Feishu's tools come from them)
		h.connectors = connector.NewRegistry(nil)
		h.registerConnectors()
	}
	register()
	golden(t, "mcp_tools.assistant", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
	// With GitHub signed in: its read tools after the hub's own, for the assistant only.
	withoutGitHub(h) // every hub registers a GitHub, signed out until the user signs in
	if err := h.addConnector(signedInGitHub{github.New(github.Config{}, "Mac", nil)}); err != nil {
		t.Fatal(err)
	}
	golden(t, "mcp_tools.assistant_with_github", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
	// With iMessage handed to Persona (the switch is on by default): its read tools too.
	register()
	if err := h.addConnector(readableIMessage{imessage.NewRemote(imessage.Config{Machine: "mac"}, "Mac", nil)}); err != nil {
		t.Fatal(err)
	}
	golden(t, "mcp_tools.assistant_with_imessage", show(h.mcpTools(true)))
	golden(t, "mcp_tools.agent", show(h.mcpTools(false)))
}

// withoutGitHub leaves the registered connectors but GitHub (the test adds one of its own).
func withoutGitHub(h *Hub) {
	r := connector.NewRegistry(nil)
	for _, c := range h.connectors.All() {
		if c.ID() != "github" {
			_ = r.Add(c)
		}
	}
	h.connectors = r
}

type readableIMessage struct{ *imessage.Remote }

func (readableIMessage) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK}
}

type signedInGitHub struct{ *github.Connector }

func (signedInGitHub) Status(context.Context) connector.Status {
	return connector.Status{State: connector.StateOK}
}
