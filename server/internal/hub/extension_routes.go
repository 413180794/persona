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

	"github.com/nwverse/persona/server/internal/extension"
)

// The plugin API (docs/extensions.md「hub 的接口」). /api/v1/plugins/<id>/… is relayed to the plugin, so the hub's own
// routes live under the reserved "-" segment: /api/v1/plugins/-/… and /api/v1/plugins/<id>/-/….

// PluginDetail is one plugin with its form, settings and permissions (GET /api/v1/plugins/{id}/-/).
type ExtensionDetail struct {
	ExtensionView
	Publisher       string                     `json:"publisher,omitempty"`
	Homepage        string                     `json:"homepage,omitempty"`
	Origin          string                     `json:"origin,omitempty"`
	PreviousVersion string                     `json:"previous_version,omitempty"`
	ReadOnly        bool                       `json:"read_only,omitempty"` // hub.json's: changed there only
	Form            *extension.Schema          `json:"form,omitempty"`
	Config          json.RawMessage            `json:"config,omitempty"`
	Secrets         []ExtensionSecretView      `json:"secrets,omitempty"`
	Permissions     []extension.PermissionLine `json:"permissions"`
	// UI are the pages it draws, opened from its settings (plugin_page, and nav_page also on the main navigation;
	// POST /api/v1/plugins/{id}/-/ui/{ui}).
	UI []UIPlace `json:"ui,omitempty"`
	// Contributions are every contribution with its switch (docs/extensions.md「贡献的开关」); contributes (the ids by
	// point) stays as it was.
	Contributions []ContributionView `json:"contributions,omitempty"`
}

// PluginSecretView is a declared secret and whether it has a value (never the value).
type ExtensionSecretView struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Set         bool   `json:"set"`
}

func (ps *extensions) detail(ctx context.Context, p *extension.Extension) ExtensionDetail {
	m := p.Full
	d := ExtensionDetail{ExtensionView: ps.view(ctx, p), Publisher: m.Publisher, Homepage: m.Homepage, Origin: p.Record.Origin,
		PreviousVersion: p.Record.PreviousVersion, ReadOnly: p.ReadOnly(), Form: p.Schema, Permissions: extension.Explain(m),
		Contributions: ps.contributionViews(p)}
	if d.Permissions == nil {
		d.Permissions = []extension.PermissionLine{}
	}
	for _, u := range ps.uiPlaces("", "") {
		if u.Extension == p.Record.ID && extension.FullPage(u.Placement) {
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
			d.Secrets = append(d.Secrets, ExtensionSecretView{Name: s.Name, Title: s.Title, Description: s.Description, Required: s.Required, Set: set[s.Name]})
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
// only its owner there (`personad plugin`) may; a phone may not (docs/extensions.md「安装」).
func (h *Hub) fromThisMachine(r *http.Request) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return remoteIP(r).IsLoopback() && tok != "" && h.cfg != nil && subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.Token)) == 1
}

func readExtensionJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, extension.ConfigMost+extension.SecretMost+4096)).Decode(v); err != nil {
		writeErrCode(w, http.StatusBadRequest, "bad_request", err)
		return false
	}
	return true
}

func (h *Hub) extensionRoutes(api *http.ServeMux) {
	h.extensionUIRoutes(api)
	h.marketRoutes(api)
	h.upgradeRoutes(api)
	h.contributionRoutes(api)
	ps := func() *extensions { return h.extensions }
	h.legacyExtensionRoutes(api, "/api/v1/plugins", "/api/v1/extensions")
	api.HandleFunc("GET /api/v1/extensions", func(w http.ResponseWriter, r *http.Request) {
		key := extKey(r, "extensions", "plugins")
		if ps() == nil {
			writeJSON(w, map[string]any{key: []ExtensionView{}})
			return
		}
		writeJSON(w, map[string]any{key: ps().listCtx(r.Context()), "can_install": ps().reg.HasStore(), "markets": h.marketStore() != nil && ps().reg.HasStore()})
	})
	// The hub's own routes under a plugin: what changes it is for a device with full rights.
	full := func(f func(w http.ResponseWriter, r *http.Request, p *extension.Extension)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if principalOf(r.Context()).Role != roleFull {
				writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
				return
			}
			var p *extension.Extension
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
				writeErrCode(w, http.StatusForbidden, "this_machine_only", errors.New("扩展只能在 hub 机器上（personad extension …）或 hub 的管理界面里装、升级和卸载"))
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
	// plugin of a registered market (its package pinned by the market's index; docs/extensions.md「扩展市场」).
	marketOr := func(w http.ResponseWriter, r *http.Request, ofMarket func() bool) bool {
		if ps() == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_plugins", errors.New("plugins are not set up"))
			return false
		}
		if h.fromThisMachine(r) || (principalOf(r.Context()).Role == roleFull && ofMarket()) {
			return true
		}
		writeErrCode(w, http.StatusForbidden, "this_machine_only", errors.New("这个扩展只能在 hub 机器上（personad extension …）或 hub 的管理界面里装、升级和卸载；手机上只能装扩展市场里的"))
		return false
	}
	api.HandleFunc("GET /api/v1/extensions/{id}/-/{$}", full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
		writeJSON(w, detailOut(r, ps().detail(r.Context(), p)))
	}))
	api.HandleFunc("PUT /api/v1/extensions/{id}/-/enabled", full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetEnabled(r.Context(), p.Record.ID, in.Enabled, actor(r)); err != nil {
			writeExtensionErr(w, http.StatusConflict, "read_only", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), ps().reg.Get(p.Record.ID))))
	}))
	api.HandleFunc("PUT /api/v1/extensions/{id}/-/config", full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
		var in struct {
			Config json.RawMessage `json:"config"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetConfig(r.Context(), p.Record.ID, in.Config, actor(r)); err != nil {
			var fe *extension.FieldError
			if errors.As(err, &fe) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": fe.Error(), "code": "invalid_config", "field": fe.Path, "message": fe.Message})
				return
			}
			writeErrCode(w, http.StatusConflict, "read_only", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), ps().reg.Get(p.Record.ID))))
	}))
	api.HandleFunc("PUT /api/v1/extensions/{id}/-/secrets/{name}", full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
		var in struct {
			Value string `json:"value"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if err := ps().reg.SetSecret(r.Context(), p.Record.ID, r.PathValue("name"), in.Value, actor(r)); err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_secret", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), ps().reg.Get(p.Record.ID))))
	}))
	api.HandleFunc("DELETE /api/v1/extensions/{id}/-/secrets/{name}", full(func(w http.ResponseWriter, r *http.Request, p *extension.Extension) {
		if err := ps().reg.DeleteSecret(r.Context(), p.Record.ID, r.PathValue("name"), actor(r)); err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_secret", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), ps().reg.Get(p.Record.ID))))
	}))

	api.HandleFunc("POST /api/v1/extensions/-/preview", owner(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Source  string `json:"source"`
			Upgrade string `json:"upgrade,omitempty"`
		}
		if !readExtensionJSON(w, r, &in) {
			return
		}
		pv, err := ps().reg.Preview(r.Context(), in.Source, in.Upgrade)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		writeJSON(w, pv)
	}))
	api.HandleFunc("POST /api/v1/extensions/-/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readExtensionJSON(w, r, &in) || !marketOr(w, r, func() bool { return ps().reg.PreviewMarketOf(in.PreviewID) != "" }) {
			return
		}
		p, err := ps().reg.Install(r.Context(), in.PreviewID, in.Digest, actorOr(r, "personad extension"))
		if err != nil {
			writeExtensionErr(w, http.StatusBadRequest, "install_failed", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), p)))
	})
	api.HandleFunc("POST /api/v1/extensions/{id}/-/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readExtensionJSON(w, r, &in) || !marketOr(w, r, func() bool { return ps().reg.PreviewMarketOf(in.PreviewID) != "" }) {
			return
		}
		p, err := ps().reg.Upgrade(r.Context(), r.PathValue("id"), in.PreviewID, in.Digest, actorOr(r, "personad extension"))
		if err != nil {
			writeExtensionErr(w, http.StatusBadRequest, "upgrade_failed", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), p)))
	})
	fromMarket := func(r *http.Request) func() bool {
		return func() bool {
			p := ps().reg.Get(r.PathValue("id"))
			return p != nil && extension.FromMarket(p.Record.Origin) != ""
		}
	}
	api.HandleFunc("POST /api/v1/extensions/{id}/-/rollback", func(w http.ResponseWriter, r *http.Request) {
		if !marketOr(w, r, fromMarket(r)) {
			return
		}
		p, err := ps().reg.Rollback(r.Context(), r.PathValue("id"), actorOr(r, "personad extension"))
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "rollback_failed", err)
			return
		}
		writeJSON(w, detailOut(r, ps().detail(r.Context(), p)))
	})
	api.HandleFunc("DELETE /api/v1/extensions/{id}/-/{$}", func(w http.ResponseWriter, r *http.Request) {
		if !marketOr(w, r, fromMarket(r)) {
			return
		}
		if err := ps().reg.Uninstall(r.Context(), r.PathValue("id"), r.URL.Query().Get("keep_data") == "1", actorOr(r, "personad extension")); err != nil {
			writeErrCode(w, http.StatusBadRequest, "uninstall_failed", err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// A plugin's program on a computer: the app's requests to it (extension_nodes.go).
	api.HandleFunc("/api/v1/extensions/{id}/-/machines/{mid}/", h.serveNodeExtensionHTTP)
	api.HandleFunc("/api/v1/extensions/{id}/", func(w http.ResponseWriter, r *http.Request) {
		// A plugin can do anything the hub can (it may hold its token): not for an answer-only device. An extension
		// gets here with the scope to call this one (extensions.call:<id>).
		if answerOnly(r) {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		id := r.PathValue("id")
		var hp *hubExtension
		if ps() != nil {
			hp = ps().get(id)
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/extensions/"+id)
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
		hp.serveExtensionHTTP(w, r)
	})
}

// listCtx is list with the request's context (for what needs the database).
func (ps *extensions) listCtx(ctx context.Context) []ExtensionView {
	out := []ExtensionView{}
	for _, p := range ps.reg.List() {
		out = append(out, ps.view(ctx, p))
	}
	return out
}
