package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Declarative UI and events (docs/plugins.md「声明式 UI」「事件」). The apps ask the hub what plugins draw where
// (GET /api/v1/plugins/-/ui) and for one of them (POST /api/v1/plugins/{id}/-/ui/{ui}); the hub asks the plugin
// (ui.render, ui.action), holds the View to what the apps draw (plugin.CheckView) and gives it to them. A launcher
// section may reach the launcher's gateway with its key the way quota does (gateway.http), never seeing the key.

const uiTimeout = 20 * time.Second

// UIPlace is one thing a plugin draws, as the apps list it.
type UIPlace struct {
	Plugin    string `json:"plugin"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Placement string `json:"placement"`
}

// uiPlaces are the plugins' UI of placement; for launcher sections, those whose hosts have the gateway at base.
func (ps *plugins) uiPlaces(placement, base string) []UIPlace {
	out := []UIPlace{}
	if ps == nil {
		return out
	}
	host := ""
	if base != "" {
		if u, err := quota.Root(base); err == nil {
			host = u.Hostname()
		}
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		hp := ps.get(p.Record.ID)
		if hp == nil || hp.proc == nil {
			continue
		}
		for _, u := range p.Manifest.UIs() {
			if u.Placement != placement || (placement == "launcher_section" && (host == "" || !hostMatches(host, u.Hosts) || !p.Record.Granted.LauncherKeys)) {
				continue
			}
			out = append(out, UIPlace{Plugin: p.Record.ID, ID: u.ID, Title: u.Title, Placement: u.Placement})
		}
	}
	return out
}

// uiRequest is what the apps send: render (no action) or an action, with where it is drawn and the plugin's state.
type uiRequest struct {
	Context struct {
		Launcher string `json:"launcher,omitempty"`
		Machine  string `json:"machine,omitempty"`
	} `json:"context"`
	State  string            `json:"state,omitempty"`
	Action string            `json:"action,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

// uiCall asks plugin id's ui contribution to render or act.
func (h *Hub) uiCall(ctx context.Context, id, uiID string, in uiRequest) (plugin.ActionResult, error) {
	var res plugin.ActionResult
	p := h.plugins.reg.Get(id)
	hp := h.plugins.get(id)
	if p == nil || !p.Record.Enabled || hp == nil || hp.proc == nil {
		return res, errNoPlugin
	}
	var item *plugin.UIItem
	for _, u := range p.Manifest.UIs() {
		if u.ID == uiID {
			u := u
			item = &u
		}
	}
	if item == nil {
		return res, fmt.Errorf("%s 没有叫 %s 的界面", id, uiID)
	}
	params := map[string]any{"state": in.State}
	if item.Placement == "launcher_section" {
		if in.Context.Launcher == "" {
			return res, errors.New("这个界面属于一个启动器：缺 context.launcher")
		}
		if !p.Record.Granted.LauncherKeys {
			return res, errors.New("这个插件没有被允许用启动器的 Key")
		}
		gw, err := h.launcherGateway(ctx, in.Context.Launcher, in.Context.Machine)
		if err != nil {
			return res, err
		}
		root, err := quota.Root(gw.base)
		if err != nil || !hostMatches(root.Hostname(), item.Hosts) {
			return res, errors.New("这个启动器的网关不是这个插件的")
		}
		var b [12]byte
		_, _ = rand.Read(b[:])
		qid := hex.EncodeToString(b[:])
		quotaQueries.Store(qid, &quotaQuery{plugin: id, send: gw.send})
		defer quotaQueries.Delete(qid)
		params["launcher"] = map[string]string{"name": in.Context.Launcher, "base_url": root.String(), "query_id": qid}
	}
	method := "ui.render"
	if in.Action != "" {
		method = "ui.action"
		params["action"], params["values"] = in.Action, in.Values
	}
	raw, _ := plugin.Targeted(params, uiID)
	cctx, cancel := context.WithTimeout(ctx, uiTimeout)
	defer cancel()
	var out json.RawMessage
	if err := hp.proc.Call(cctx, method, raw, &out); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return res, errors.New(re.Message)
		}
		return res, err
	}
	if method == "ui.render" {
		v, err := plugin.ParseView(out)
		if err != nil {
			return res, fmt.Errorf("插件画的界面这个 hub 画不了：%v", err)
		}
		res.View = v
		return res, nil
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return res, err
	}
	if res.View != nil {
		if err := plugin.CheckView(res.View); err != nil {
			return plugin.ActionResult{}, fmt.Errorf("插件画的界面这个 hub 画不了：%v", err)
		}
	}
	if res.OpenURL != "" {
		if u, err := url.Parse(res.OpenURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return plugin.ActionResult{}, errors.New("插件要打开的不是 https 链接")
		}
	}
	return res, nil
}

var errNoPlugin = errors.New("no such plugin")

func (h *Hub) pluginUIRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/plugins/-/ui", func(w http.ResponseWriter, r *http.Request) {
		placement := r.URL.Query().Get("placement")
		base := ""
		if name := r.URL.Query().Get("launcher"); name != "" && placement == "launcher_section" {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			gw, err := h.launcherGateway(ctx, name, r.URL.Query().Get("machine"))
			cancel()
			if err == nil {
				base = gw.base
			}
		}
		writeJSON(w, map[string]any{"ui": h.plugins.uiPlaces(placement, base)})
	})
	api.HandleFunc("POST /api/v1/plugins/{id}/-/ui/{ui}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		var in uiRequest
		if !readPluginJSON(w, r, &in) {
			return
		}
		res, err := h.uiCall(r.Context(), r.PathValue("id"), r.PathValue("ui"), in)
		switch {
		case errors.Is(err, errNoPlugin):
			writeErrCode(w, http.StatusNotFound, "not_found", err)
		case errors.Is(err, errOffline):
			writeErrCode(w, http.StatusServiceUnavailable, "offline", err)
		case err != nil:
			writeErrCode(w, http.StatusBadGateway, "plugin_failed", err)
		default:
			writeJSON(w, res)
		}
	})
}

// emit tells the plugins subscribed to topic (running ones: events need on_startup) about it; never waits.
func (ps *plugins) emit(topic string, payload map[string]any) {
	if ps == nil {
		return
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		subscribed := false
		for _, t := range p.Manifest.Topics() {
			subscribed = subscribed || t == topic
		}
		if !subscribed {
			continue
		}
		if hp := ps.get(p.Record.ID); hp != nil && hp.proc != nil {
			_ = hp.proc.Notify("event."+topic, payload)
		}
	}
}
