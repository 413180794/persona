package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/connector/github"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// Connectors: each source of the user's information plugs in as a connector, and the hub's pipes pick up what it
// implements — the assistant's tools (writes behind the gate), 设置 › 连接器's status, realtime kicks
// (internal/connector, docs/plan-connectors.md).

// addConnector registers c, refusing one that offers a tool the hub has itself ("kb_search", "phone_…"): calls go
// to the connector first. A connector may share a prefix with the hub's tools ("lark") as long as it does not offer
// them; the hub's own then move to it (docs/plan-connectors.md M4).
func (h *Hub) addConnector(c connector.Connector) error {
	for _, t := range connector.ToolNames(c) {
		for _, b := range h.builtinTools() {
			if t == b || strings.HasSuffix(b, "_") && strings.HasPrefix(t, b) {
				return fmt.Errorf("connector %q: %s would shadow the hub's own", c.ID(), t)
			}
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
		writeJSON(w, map[string]any{"connectors": h.listedConnectors(r.Context())})
	})
	h.imessageRoutes(api)
}

// startConnectors registers the connectors and runs them.
func (h *Hub) startConnectors(ctx context.Context) {
	h.registerConnectors()
	h.connectors.Start(ctx, h.connectorChanged, h.logf)
}

// registerConnectors registers Feishu (first: the knowledge base reads its sources in this order) and the
// connectors hub.json sets up.
func (h *Hub) registerConnectors() {
	if h.lark != nil {
		if err := h.addConnector(larkConnector{h}); err != nil {
			h.logf("lark connector: %v", err)
		}
	}
	if h.mail != nil {
		if err := h.addConnector(mailConnector{h}); err != nil {
			h.logf("mail connector: %v", err)
		}
	}
	if gh := h.cfg.GitHub; gh.Configured() {
		name := gh.Auth.FromMachine
		if m := h.byID[name]; m != nil {
			name = m.cfg.Name
		}
		if err := h.addConnector(github.New(*gh, name, h.ghToken(gh.Auth.FromMachine))); err != nil {
			h.logf("github connector: %v", err)
		}
	}
	if im := h.cfg.IMessage; im.Configured() {
		if err := h.addIMessage(*im); err != nil {
			h.logf("imessage connector: %v", err)
		}
	}
}

// ghToken reads the token of machine's gh login (credential.gh_token); it never goes to a log.
func (h *Hub) ghToken(machine string) github.TokenSource {
	return func(ctx context.Context, host string) (string, error) {
		m := h.byID[machine]
		if m == nil {
			return "", github.ErrNoMachine
		}
		var out struct {
			Token string `json:"token"`
		}
		err := m.call(ctx, "credential.gh_token", map[string]any{"host": host}, &out)
		var re *rpc.Error
		switch {
		case errors.Is(err, errOffline):
			return "", github.ErrOffline
		case errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method"):
			return "", github.ErrNodeTooOld
		case errors.As(err, &re) && re.Code == proto.CodeNoGh:
			return "", github.ErrNoGh
		case errors.As(err, &re) && re.Code == proto.CodeNotSignedIn:
			return "", github.ErrNotSignedIn
		case err != nil:
			return "", fmt.Errorf("%s: %s", m.cfg.Name, github.Scrub(err.Error()))
		case out.Token == "":
			return "", github.ErrNotSignedIn
		}
		return out.Token, nil
	}
}
