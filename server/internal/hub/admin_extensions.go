package hub

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
)

// The plugins page (docs/admin.md「插件」, docs/plugins.md「管理界面」): what `personad plugin` does on the hub's
// machine, from a signed-in admin session (CSRF and the admin scope already checked by adminRoutes). Installing runs
// code on this machine; the admin password guards everything else here that is as strong (adding computers, issuing
// credentials), so the session may. Every change is audited as the admin UI.

// adminActor is who the plugin audit names for a change made here.
const adminActor = "admin UI"

// adminUploadPath is where the browser uploads a package: the one admin write whose body is not JSON.
const adminUploadPath = "/api/v1/admin/plugins/upload"

// Previews stage packages of up to plugin.PackageMost bytes on disk for 10 minutes: one at a time, and at most
// pluginPreviewsPer in pluginPreviewWindow, so a stolen session cannot fill the disk.
const (
	extensionPreviewsPer   = 10
	extensionPreviewWindow = 10 * time.Minute
)

// pluginGate holds previews to one at a time and pluginPreviewsPer a window.
type extensionGate struct {
	mu     sync.Mutex
	busy   bool
	recent []time.Time
}

// begin takes the gate: end releases it. busy: another preview is running; retry > 0: too many lately.
func (g *extensionGate) begin(now time.Time) (end func(), retry time.Duration, busy bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy {
		return nil, 0, true
	}
	kept := g.recent[:0]
	for _, t := range g.recent {
		if now.Sub(t) < extensionPreviewWindow {
			kept = append(kept, t)
		}
	}
	g.recent = kept
	if len(g.recent) >= extensionPreviewsPer {
		return nil, g.recent[0].Add(extensionPreviewWindow).Sub(now), false
	}
	g.recent = append(g.recent, now)
	g.busy = true
	return func() {
		g.mu.Lock()
		g.busy = false
		g.mu.Unlock()
	}, 0, false
}

// AdminPlugins is GET /api/v1/admin/plugins.
type AdminExtensions struct {
	Extensions []ExtensionDetail `json:"plugins"`
	CanInstall bool              `json:"can_install"`
	// UploadMost is the largest package the browser may upload, in bytes.
	UploadMost int64 `json:"upload_most"`
}

func (h *Hub) adminExtensionRoutes(mux *http.ServeMux) {
	ps := func(w http.ResponseWriter) *extensions {
		if h.extensions == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_plugins", errors.New("这台 hub 没有启用插件"))
		}
		return h.extensions
	}
	// one is the installed or known plugin {id}, or a 404.
	one := func(f func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s := ps(w)
			if s == nil {
				return
			}
			p := s.reg.Get(r.PathValue("id"))
			if p == nil {
				writeErrCode(w, http.StatusNotFound, "not_found", errors.New("没有这个插件"))
				return
			}
			f(w, r, s, p)
		}
	}
	// gated runs a preview under the gate.
	gated := func(w http.ResponseWriter, run func() (*extension.Preview, error)) {
		end, retry, busy := h.admin.extensions.begin(h.adminNow())
		switch {
		case busy:
			writeErrCode(w, http.StatusConflict, "busy", errors.New("正在预览另一个插件，等它结束再试"))
			return
		case retry > 0:
			retryAfter(w, retry)
			writeErrorBody(w, http.StatusTooManyRequests, map[string]any{
				"error": "too many previews", "code": "rate_limited", "retry_after": max(1, int((retry+time.Second-1)/time.Second)),
			})
			return
		}
		defer end()
		pv, err := run()
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		h.logf("admin: previewed plugin %s %s (%s)", pv.Manifest.ID, pv.Manifest.Version, map[bool]string{true: "upgrade", false: "install"}[pv.Upgrade])
		writeJSON(w, pv)
	}

	// The markets, as the apps have them (the admin session is the gate).
	h.marketRoutesAt(mux, "/api/v1/admin/plugins/-", func(w http.ResponseWriter, r *http.Request) bool {
		if h.extensions == nil {
			writeErrCode(w, http.StatusServiceUnavailable, "no_plugins", errors.New("plugins are not set up"))
			return false
		}
		return true
	})
	mux.HandleFunc("GET /api/v1/admin/plugins", func(w http.ResponseWriter, r *http.Request) {
		out := AdminExtensions{Extensions: []ExtensionDetail{}, UploadMost: extension.PackageMost}
		if h.extensions != nil {
			for _, p := range h.extensions.reg.List() {
				out.Extensions = append(out.Extensions, h.extensions.detail(r.Context(), p))
			}
			out.CanInstall = h.extensions.reg.HasStore()
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/v1/admin/plugins/{id}", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		writeJSON(w, ps.detail(r.Context(), p))
	}))
	mux.HandleFunc("POST /api/v1/admin/plugins/preview", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Source  string `json:"source"`
			Upgrade string `json:"upgrade,omitempty"`
		}
		if !readAdminJSON(w, r, &in) {
			return
		}
		s := ps(w)
		if s == nil {
			return
		}
		gated(w, func() (*extension.Preview, error) { return s.reg.Preview(r.Context(), in.Source, in.Upgrade) })
	})
	// The package itself as the body (application/octet-stream), its file name and what it upgrades in the query.
	mux.HandleFunc("POST "+adminUploadPath, func(w http.ResponseWriter, r *http.Request) {
		s := ps(w)
		if s == nil {
			return
		}
		if r.ContentLength > extension.PackageMost {
			writeErrCode(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Errorf("包超过 %d MB", extension.PackageMost>>20))
			return
		}
		q := r.URL.Query()
		if _, err := extension.UploadName(q.Get("name")); err != nil {
			writeErrCode(w, http.StatusBadRequest, "invalid_plugin", err)
			return
		}
		body := http.MaxBytesReader(w, r.Body, extension.PackageMost+1)
		gated(w, func() (*extension.Preview, error) {
			return s.reg.PreviewUpload(r.Context(), q.Get("name"), body, q.Get("upgrade"))
		})
	})
	mux.HandleFunc("POST /api/v1/admin/plugins/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readAdminJSON(w, r, &in) {
			return
		}
		s := ps(w)
		if s == nil {
			return
		}
		if in.Digest == "" {
			writeErrCode(w, http.StatusBadRequest, "confirm_required", errors.New("先看过它能做什么，确认后带上 permissions_digest 安装"))
			return
		}
		p, err := s.reg.Install(r.Context(), in.PreviewID, in.Digest, adminActor)
		if err != nil {
			writeExtensionErr(w, http.StatusBadRequest, "install_failed", err)
			return
		}
		h.logf("admin: installed plugin %s %s", p.Record.ID, p.Record.Version)
		writeJSON(w, s.detail(r.Context(), p))
	})
	mux.HandleFunc("POST /api/v1/admin/plugins/{id}/upgrade", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		var in struct {
			PreviewID string `json:"preview_id"`
			Digest    string `json:"permissions_digest"`
		}
		if !readAdminJSON(w, r, &in) {
			return
		}
		np, err := ps.reg.Upgrade(r.Context(), p.Record.ID, in.PreviewID, in.Digest, adminActor)
		if err != nil {
			writeExtensionErr(w, http.StatusBadRequest, "upgrade_failed", err)
			return
		}
		h.logf("admin: upgraded plugin %s %s → %s", np.Record.ID, np.Record.PreviousVersion, np.Record.Version)
		writeJSON(w, ps.detail(r.Context(), np))
	}))
	mux.HandleFunc("POST /api/v1/admin/plugins/{id}/rollback", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		np, err := ps.reg.Rollback(r.Context(), p.Record.ID, adminActor)
		if err != nil {
			writeErrCode(w, http.StatusBadRequest, "rollback_failed", err)
			return
		}
		h.logf("admin: rolled plugin %s back to %s", np.Record.ID, np.Record.Version)
		writeJSON(w, ps.detail(r.Context(), np))
	}))
	mux.HandleFunc("PUT /api/v1/admin/plugins/{id}/enabled", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !readAdminJSON(w, r, &in) {
			return
		}
		if err := ps.reg.SetEnabled(r.Context(), p.Record.ID, in.Enabled, adminActor); err != nil {
			writeExtensionErr(w, http.StatusConflict, "read_only", err)
			return
		}
		writeJSON(w, ps.detail(r.Context(), ps.reg.Get(p.Record.ID)))
	}))
	mux.HandleFunc("DELETE /api/v1/admin/plugins/{id}", one(func(w http.ResponseWriter, r *http.Request, ps *extensions, p *extension.Extension) {
		var in struct {
			KeepData bool `json:"keep_data"`
		}
		if !readAdminJSON(w, r, &in) {
			return
		}
		if err := ps.reg.Uninstall(r.Context(), p.Record.ID, in.KeepData, adminActor); err != nil {
			writeErrCode(w, http.StatusConflict, "uninstall_failed", err)
			return
		}
		h.logf("admin: uninstalled plugin %s (keep data: %v)", p.Record.ID, in.KeepData)
		writeJSON(w, map[string]any{"ok": true})
	}))
}
