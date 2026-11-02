package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Connectors: each source of the user's information plugs in as a connector, and the hub's pipes pick up what it
// implements — the assistant's tools (writes behind the gate), 设置 › 连接器's status, realtime kicks
// (internal/connector, docs/plan-connectors.md).

// addConnector registers c, refusing an id whose tools would shadow the hub's own ("kb_…", "phone_…").
func (h *Hub) addConnector(c connector.Connector) error {
	for _, t := range h.builtinTools() {
		if strings.HasPrefix(t, c.ID()+"_") {
			return fmt.Errorf("connector %q: its tools would shadow the hub's %s", c.ID(), t)
		}
	}
	return h.connectors.Add(c)
}

// builtinTools are the names of every tool the hub offers besides connectors', whether or not it is set up to offer
// them now (the phone's and mail's by their prefix: they come and go with the app and the accounts).
func (h *Hub) builtinTools() []string {
	names := []string{"phone_", "mail_"}
	for _, t := range assistantTools(true) {
		names = append(names, t.Name)
	}
	return names
}

// connectorChanged is a connector's realtime event: collect the inbox or archive soon.
func (h *Hub) connectorChanged(id, scope string) {
	switch scope {
	case connector.ScopeInbox:
		h.kickInbox()
	case connector.ScopeChats, connector.ScopeItems:
		h.kbKick()
	}
}

// callConnector runs a connector's tool for machineID's assistant; a write the gate leaves to the user is asked on
// the phone like mail_send.
func (h *Hub) callConnector(ctx context.Context, machineID, name string, args json.RawMessage) proto.ToolResult {
	return h.connectors.Call(ctx, name, args, func(ctx context.Context, summary string, input json.RawMessage) bool {
		return h.approve(ctx, machineID, name, summary, input)
	})
}

func (h *Hub) connectorRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/connectors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"connectors": h.connectors.Infos(r.Context())})
	})
}
