package hub

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/plugin"
)

// The plugin API (docs/plugins.md「hub 的接口」). /api/v1/plugins/<id>/… is relayed to the plugin, so the hub's own
// routes live under the reserved "-" segment: /api/v1/plugins/-/… and /api/v1/plugins/<id>/-/….

// PluginDetail is one plugin with its form, settings and permissions (GET /api/v1/plugins/{id}/-/).
type PluginDetail struct {
	PluginView
	Publisher       string                  `json:"publisher,omitempty"`
	Homepage        string                  `json:"homepage,omitempty"`
	Origin          string                  `json:"origin,omitempty"`
	PreviousVersion string                  `json:"previous_version,omitempty"`
	ReadOnly        bool                    `json:"read_only,omitempty"` // hub.json's: changed there only
	Form            *plugin.Schema          `json:"form,omitempty"`
	Config          json.RawMessage         `json:"config,omitempty"`
	Secrets         []PluginSecretView      `json:"secrets,omitempty"`
	Permissions     []plugin.PermissionLine `json:"permissions"`
	// UI are the pages it draws, opened from its settings (plugin_page, and nav_page also on the main navigation;
	// POST /api/v1/plugins/{id}/-/ui/{ui}).
	UI []UIPlace `json:"ui,omitempty"`
}

// PluginSecretView is a declared secret and whether it has a value (never the value).
type PluginSecretView struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Set         bool   `json:"set"`
}

func (ps *plugins) detail(ctx context.Context, p *plugin.Plugin) PluginDetail {
	m := p.Manifest
	d := PluginDetail{PluginView: ps.view(ctx, p), Publisher: m.Publisher, Homepage: m.Homepage, Origin: p.Record.Origin,
		PreviousVersion: p.Record.PreviousVersion, ReadOnly: p.ReadOnly(), Form: p.Schema, Permissions: plugin.Explain(m)}
	if d.Permissions == nil {
		d.Permissions = []plugin.PermissionLine{}
	}
	for _, u := range ps.uiPlaces("", "") {
		if u.Plugin == p.Record.ID && (u.Placement == "plugin_page" || u.Placement == "nav_page") {
			d.UI = append(d.UI, u)
		}
	}
	if !p.ReadOnly() { // hub.json's settings may hold keys: never sent
		d.Config = p.Record.Config
		if len(d.Config) == 0 {
			d.Config = json.RawMessage(`{}`)
		}
		set := ps.reg.SecretsSet(ctx, p)
		for _, s := range m.Secrets {
			d.Secrets = append(d.Secrets, PluginSecretView{Name: s.Name, Title: s.Title, Description: s.Description, Required: s.Required, Set: set[s.Name]})
		}
	}
	return d
}

// actorOr is actor, or def for the master token over loopback.
func actorOr(r *http.Request, def string) string {
	if principalOf(r.Context()).ID == "" {
		return def
	}
	return actor(r)
}

// actor is who changes a plugin, for its audit.
func actor(r *http.Request) string {
	p := principalOf(r.Context())
	if p.ID == "" {
		return "hub token"
	}
	return "device:" + p.ID
}

// fromThisMachine says whether r carries the master token over loopback: installing runs code on this machine, so
// only its owner there (`personad plugin`) may; a phone may not (docs/plugins.md「安装」).
func (h *Hub) fromThisMachine(r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return remoteIP(r).IsLoopback() && tok != "" && h.cfg != nil && subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.Token)) == 1
}

func readPluginJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, plugin.ConfigMost+plugin.SecretMost+4096)).Decode(v); err != nil {
		writeErrCode(w, http.StatusBadRequest, "bad_request", err)
		return false
	}
	return true
}

func (h *Hub) pluginRoutes(api *http.ServeMux) {
	h.pluginUIRoutes(api)
	h.marketRoutes(api)
	ps := func() *plugins { return h.plugins }
	api.HandleFunc("GET /api/v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		if ps() == nil {
			writeJSON(w, map[string]any{"plugins": []PluginView{}})
			return
		}
		writeJSON(w, map[string]any{"plugins": ps().listCtx(r.Context()), "can_install": ps().reg.HasStore(), "markets": h.marketStore() != nil && ps().reg.HasStore()})
	})
	// The hub's own routes under a plugin: what changes it is for a device with full rights.
	full := func(f func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if principalOf(r.Context()).Role != roleFull {
				writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
				return
			}
			var p *plugin.Plugin
			if ps() != nil {
				p = ps().reg.Get(r.PathValue("id"))
			}
			if p == nil {
				writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such plugin"))
				return
			}
			f(w, r, p)
		}
	}
	// Installing, upgrading and removing run or delete code on this machine: the master token over loopback only.
	owner := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !h.fromThisMachine(r) {
				writeErrCode(w, http.StatusForbidden, "this_machine_only", errors.New("插件只能在 hub 机器上（personad plugin …）或 hub 的管理界面里装、升级和卸载"))
				return
			}
			if ps() == nil {
				writeErrCode(w, http.StatusServiceUnavailable, "no_plugins", errors.New("plugins are not set up"))
				return
			}
			f(w, r)
		}
	}
	// marketOr is owner for installing, upgrading, rolling back and removing, or a device with full rights when it is a
	// plugin of a registered market (its package pinned by the market's index; docs/plugins.md「插件市场」).
	marketOr := func(w http.ResponseWriter, r *http.Request, ofMarket func() bool) bool {
		if ps() == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_plugins", errors.New("plugins are not set up"))
			return false
		}
		if h.fromThisMachine(r) || (principalOf(r.Context()).Role == roleFull && ofMarket()) {
			return true
		}
		writeErrCode(w, http.StatusForbidden, "this_machine_only", errors.New("这个插件只能在 hub 机器上（personad plugin …）或 hub 的管理界面里装、升级和卸载；手机上只能装插件市场里的"))
		return false
	}
	api.HandleFunc("GET /api/v1/plugins/{id}/-/{$}", full(func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin) {
		writeJSON(w, ps().detail(r.Context(), p))
	}))
	api.HandleFunc("PUT /api/v1/plugins/{id}/-/enabled", full(func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetEnabled(r.Context(), p.Record.ID, in.Enabled, actor(r)); err != nil {
			writeErrCode(w, http.StatusConflict, "read_only", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), ps().reg.Get(p.Record.ID)))
	}))
	api.HandleFunc("PUT /api/v1/plugins/{id}/-/config", full(func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin) {
		var in struct {
			Config json.RawMessage `json:"config"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetConfig(r.Context(), p.Record.ID, in.Config, actor(r)); err != nil {
			var fe *plugin.FieldError
			if errors.As(err, &fe) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": fe.Error(), "code": "invalid_config", "field": fe.Path, "message": fe.Message})
				return
			}
			writeErrCode(w, http.StatusConflict, "read_only", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), ps().reg.Get(p.Record.ID)))
	}))
	api.HandleFunc("PUT /api/v1/plugins/{id}/-/secrets/{name}", full(func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin) {
		var in struct {
			Value string `json:"value"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetSecret(r.Context(), p.Record.ID, r.PathValue("name"), in.Value, actor(r)); err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_secret", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), ps().reg.Get(p.Record.ID)))
	}))
	api.HandleFunc("DELETE /api/v1/plugins/{id}/-/secrets/{name}", full(func(w http.ResponseWriter, r *http.Request, p *plugin.Plugin) {
		if err := ps().reg.DeleteSecret(r.Context(), p.Record.ID, r.PathValue("name"), actor(r)); err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_secret", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), ps().reg.Get(p.Record.ID)))
	}))

	api.HandleFunc("POST /api/v1/plugins/-/preview", owner(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Source  string `json:"source"`
			Upgrade string `json:"upgrade,omitempty"`
		}
		if !readPluginJSON(w, r, &in) {
			return
		}
		pv, err := ps().reg.Preview(r.Context(), in.Source, in.Upgrade)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		writeJSON(w, pv)
	}))
	api.HandleFunc("POST /api/v1/plugins/-/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readPluginJSON(w, r, &in) || !marketOr(w, r, func() bool { return ps().reg.PreviewMarketOf(in.PreviewID) != "" }) {
			return
		}
		p, err := ps().reg.Install(r.Context(), in.PreviewID, in.Digest, actorOr(r, "personad plugin"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "install_failed", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), p))
	})
	api.HandleFunc("POST /api/v1/plugins/{id}/-/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readPluginJSON(w, r, &in) || !marketOr(w, r, func() bool { return ps().reg.PreviewMarketOf(in.PreviewID) != "" }) {
			return
		}
		p, err := ps().reg.Upgrade(r.Context(), r.PathValue("id"), in.PreviewID, in.Digest, actorOr(r, "personad plugin"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "upgrade_failed", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), p))
	})
	fromMarket := func(r *http.Request) func() bool {
		return func() bool {
			p := ps().reg.Get(r.PathValue("id"))
			return p != nil && plugin.FromMarket(p.Record.Origin) != ""
		}
	}
	api.HandleFunc("POST /api/v1/plugins/{id}/-/rollback", func(w http.ResponseWriter, r *http.Request) {
		if !marketOr(w, r, fromMarket(r)) {
			return
		}
		p, err := ps().reg.Rollback(r.Context(), r.PathValue("id"), actorOr(r, "personad plugin"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "rollback_failed", err)
			return
		}
		writeJSON(w, ps().detail(r.Context(), p))
	})
	api.HandleFunc("DELETE /api/v1/plugins/{id}/-/{$}", func(w http.ResponseWriter, r *http.Request) {
		if !marketOr(w, r, fromMarket(r)) {
			return
		}
		if err := ps().reg.Uninstall(r.Context(), r.PathValue("id"), r.URL.Query().Get("keep_data") == "1", actorOr(r, "personad plugin")); err != nil {
			writeErrCode(w, http.StatusBadRequest, "uninstall_failed", err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	api.HandleFunc("/api/v1/plugins/{id}/", func(w http.ResponseWriter, r *http.Request) {
		// A plugin can do anything the hub can (it may hold its token): not for an answer-only device.
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		id := r.PathValue("id")
		var hp *hubPlugin
		if ps() != nil {
			hp = ps().get(id)
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"+id)
		if hp == nil || hp.proxy == nil || hp.proc == nil || strings.HasPrefix(rest, "/-/") {
			writeErrCode(w, http.StatusNotFound, "not_found", errors.New("no such plugin"))
			return
		}
		end, err := hp.proc.BeginHTTP(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, fmt.Errorf("plugin %s is not answering: %w", id, err))
			return
		}
		defer end()
		hp.servePluginHTTP(w, r)
	})
}

// listCtx is list with the request's context (for what needs the database).
func (ps *plugins) listCtx(ctx context.Context) []PluginView {
	out := []PluginView{}
	for _, p := range ps.reg.List() {
		out = append(out, ps.view(ctx, p))
	}
	return out
}
