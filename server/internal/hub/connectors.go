package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
// them now (the phone's by their prefix: they come and go with the app).
func (h *Hub) builtinTools() []string {
	names := []string{"phone_"}
	for _, t := range assistantTools() {
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
	// Signing in, a step at a time (connector.Login): the app shows each Prompt's fields and sends them back. The
	// values (a phone number, a code, a password) go to the connector only: never into a log or a response.
	api.HandleFunc("POST /api/v1/connectors/{id}/login", func(w http.ResponseWriter, r *http.Request) {
		l, ok := h.connectorLogin(w, r.PathValue("id"))
		if !ok {
			return
		}
		var body struct {
			Step   string            `json:"step"`
			Values map[string]string `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		p, err := l.LoginStep(r.Context(), body.Step, body.Values)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, p)
	})
	api.HandleFunc("POST /api/v1/connectors/{id}/logout", func(w http.ResponseWriter, r *http.Request) {
		l, ok := h.connectorLogin(w, r.PathValue("id"))
		if !ok {
			return
		}
		if err := l.Logout(r.Context()); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	// 设置 › 连接器's switch: off, the connector stops and brings nothing in (Registry.SetEnabled); kept in settings.
	api.HandleFunc("PUT /api/v1/connectors/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if h.connectors.Get(id) == nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
			return
		}
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if body.Enabled != nil {
			if err := h.setConnectorEnabled(r.Context(), id, *body.Enabled); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		for _, info := range h.connectors.Infos(r.Context()) {
			if info.ID == id {
				writeJSON(w, info)
				return
			}
		}
	})
	h.imessageRoutes(api)
}

// connectorLogin is connector id's sign-in, or writes why there is none.
func (h *Hub) connectorLogin(w http.ResponseWriter, id string) (connector.Login, bool) {
	c := h.connectors.Get(id)
	if c == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no connector %q", id))
		return nil, false
	}
	l, ok := c.(connector.Login)
	if !ok || !slices.Contains(connector.Capabilities(c), "login") {
		writeErr(w, http.StatusConflict, fmt.Errorf("%s has no sign-in", c.Name()))
		return nil, false
	}
	return l, true
}

// connectorSettingsKey holds the user's switch per connector: {"switch": {"telegram": true}}.
const connectorSettingsKey = "connectors"

type connectorSettings struct {
	Switch map[string]bool `json:"switch,omitempty"`
}

// setConnectorEnabled switches a connector and keeps the choice for the next start.
func (h *Hub) setConnectorEnabled(ctx context.Context, id string, on bool) error {
	if err := h.connectors.SetEnabled(id, on); err != nil {
		return err
	}
	if h.store == nil {
		return nil
	}
	var cs connectorSettings
	if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
		return err
	}
	if cs.Switch == nil {
		cs.Switch = map[string]bool{}
	}
	cs.Switch[id] = on
	return h.store.PutSetting(ctx, connectorSettingsKey, cs)
}

// restoreConnectorSwitches applies the user's switches before the connectors start. One the user never switched is
// on, unless it was added to start off (addConnectorOff: Telegram waits for the user to turn it on and sign in).
func (h *Hub) restoreConnectorSwitches(ctx context.Context) {
	var cs connectorSettings
	if h.store != nil {
		if _, err := h.store.Setting(ctx, connectorSettingsKey, &cs); err != nil {
			h.logf("connectors: settings: %v", err)
		}
	}
	for _, c := range h.connectors.All() {
		on, chosen := cs.Switch[c.ID()]
		if !chosen {
			on = !h.connectorsOff[c.ID()]
		}
		if !on {
			_ = h.connectors.SetEnabled(c.ID(), false)
		}
	}
}

// addConnectorOff registers a connector that starts switched off until the user turns it on.
func (h *Hub) addConnectorOff(c connector.Connector) error {
	if err := h.addConnector(c); err != nil {
		return err
	}
	if h.connectorsOff == nil {
		h.connectorsOff = map[string]bool{}
	}
	h.connectorsOff[c.ID()] = true
	return nil
}

// connectorSecrets is where connector id keeps its secret (connector.Secrets): one row of connector_secrets, sealed
// with secret.key like launchers' keys and mail passwords. Each connector is handed its own; the plain text is only
// ever in memory.
type connectorSecrets struct {
	h  *Hub
	id string
}

func (h *Hub) secretsFor(id string) connector.Secrets { return connectorSecrets{h: h, id: id} }

var errNoDatabase = errors.New("no database: nothing can be kept")

func (s connectorSecrets) Load(ctx context.Context) ([]byte, error) {
	if s.h.store == nil {
		return nil, errNoDatabase
	}
	sealed, ok, err := s.h.store.ConnectorSecret(ctx, s.id)
	if err != nil || !ok {
		return nil, err
	}
	plain, err := s.h.openKey(sealed)
	if err != nil {
		return nil, fmt.Errorf("%s: the stored secret does not open (secret.key replaced?)", s.id)
	}
	return []byte(plain), nil
}

func (s connectorSecrets) Save(ctx context.Context, data []byte) error {
	if s.h.store == nil {
		return errNoDatabase
	}
	sealed, err := s.h.sealKey(string(data))
	if err != nil {
		return err
	}
	return s.h.store.PutConnectorSecret(ctx, s.id, sealed)
}

func (s connectorSecrets) Delete(ctx context.Context) error {
	if s.h.store == nil {
		return nil
	}
	return s.h.store.DeleteConnectorSecret(ctx, s.id)
}

// startConnectors registers the connectors, applies the user's switches and runs them.
func (h *Hub) startConnectors(ctx context.Context) {
	h.registerConnectors()
	h.restoreConnectorSwitches(ctx)
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
