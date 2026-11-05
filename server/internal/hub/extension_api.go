package hub

import (
	"encoding/json"
	"github.com/nwverse/persona/server/internal/extension"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/proto"
)

// The api extension point (docs/extensions.md「App 接口」): a plugin serves a part of the app's API under
// /api/v1/<prefix>/ on its HTTP side. The hub authenticates the request, then hands it over: before its own routes for
// a prefix being moved out of the hub (migratablePrefixes: the plugin's wins), after them otherwise (a plugin cannot
// take the hub's own API). It adds the plugin's features to the hub's and passes its events (api.broadcast) to the
// apps.

// migratablePrefixes are the hub's own prefixes a plugin may take over while they move out of the hub.
var migratablePrefixes = []string{"lark"}

// apiRoute is the running plugin serving path (nil when none does) and the contribution the path is under.
func (ps *extensions) apiRoute(path string) (*hubExtension, extension.APIItem) {
	if ps == nil || !strings.HasPrefix(path, "/api/v1/") {
		return nil, extension.APIItem{}
	}
	seg, _, _ := strings.Cut(strings.TrimPrefix(path, "/api/v1/"), "/")
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		for _, a := range p.Manifest.APIs() {
			if a.Prefix != seg {
				continue
			}
			if hp := ps.get(p.Record.ID); hp != nil && hp.proc != nil && hp.proxy != nil {
				return hp, a
			}
		}
	}
	return nil, extension.APIItem{}
}

// pluginAPIs serves the requests for a plugin's part of the API; the rest goes to next (the hub's own routes, mux).
func (h *Hub) extensionAPIs(next http.Handler, mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hp, item := h.extensions.apiRoute(r.URL.Path)
		if hp == nil {
			next.ServeHTTP(w, r)
			return
		}
		seg, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
		if !slices.Contains(migratablePrefixes, seg) && hubOwns(mux, seg) {
			next.ServeHTTP(w, r) // the hub's own API is not a plugin's to take, any of it
			return
		}
		end, err := hp.proc.BeginHTTP(r.Context())
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		defer end()
		// Who is asking, for the plugin (it never gets the credential); never what the caller says it is.
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-persona-") {
				r.Header.Del(k)
			}
		}
		p := principalOf(r.Context())
		r.Header.Set("X-Persona-Device", p.ID)
		r.Header.Set("X-Persona-Role", p.Role)
		// A send it declared a receipt for: kept like the hub's own (GET /api/v1/ops/{id}), a retry reaches it once.
		under := strings.TrimPrefix(r.URL.Path, "/api/v1/"+item.Prefix)
		for _, rc := range item.Receipts {
			if rc.Matches(r.Method, under) {
				h.withReplyReceipt(rc.Kind, hp.serveExtensionHTTP)(w, r)
				return
			}
		}
		hp.serveExtensionHTTP(w, r)
	})
}

// features are the hub's features with those of the plugins serving part of the API.
func (h *Hub) features() []string {
	out := slices.Clone(proto.Features)
	if h.extensions == nil {
		return out
	}
	if len(h.extensions.messagingSources()) > 0 && !slices.Contains(out, messagingFeature) {
		out = append(out, messagingFeature)
	}
	for _, p := range h.extensions.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		hp := h.extensions.get(p.Record.ID)
		if hp == nil || hp.proc == nil {
			continue
		}
		for _, a := range p.Manifest.APIs() {
			for _, f := range a.Features {
				if !slices.Contains(out, f) {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// apiBroadcast passes a plugin's event to the apps: only of a type its api contributions declare.
func (ps *extensions) apiBroadcast(id string, params json.RawMessage) {
	if ps == nil || ps.h == nil {
		return
	}
	p := ps.reg.Get(id)
	if p == nil {
		return
	}
	var ev map[string]any
	if json.Unmarshal(params, &ev) != nil {
		return
	}
	typ, _ := ev["type"].(string)
	delete(ev, "contribution")
	for _, a := range p.Manifest.APIs() {
		if slices.Contains(a.Events, typ) {
			ps.h.broadcast(ev)
			return
		}
	}
	ps.logf("plugin %s: api.broadcast of a type it did not declare (%q): dropped", id, typ)
}

// hubOwns says whether the hub has any route under /api/v1/<prefix>: probed once per prefix.
func hubOwns(mux *http.ServeMux, prefix string) bool {
	if v, ok := ownedPrefixes.Load(prefix); ok {
		return v.(bool)
	}
	owned := false
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, p := range []string{"/api/v1/" + prefix, "/api/v1/" + prefix + "/", "/api/v1/" + prefix + "/x", "/api/v1/" + prefix + "/x/y"} {
			req, _ := http.NewRequest(method, p, nil)
			if _, pattern := mux.Handler(req); pattern != "" {
				owned = true
			}
		}
	}
	ownedPrefixes.Store(prefix, owned)
	return owned
}

var ownedPrefixes sync.Map
