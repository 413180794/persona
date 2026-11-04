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
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/proc"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Declarative UI and events (docs/plugins.md「声明式 UI」「事件」). The apps ask the hub what plugins draw where
// (GET /api/v1/plugins/-/ui) and for one of them (POST /api/v1/plugins/{id}/-/ui/{ui}); the hub asks the plugin
// (ui.render, ui.action), holds the View to what the apps draw (plugin.CheckView) and gives it to them. A launcher
// section may reach the launcher's gateway with its key the way quota does (gateway.http), never seeing the key. An
// inbox item's page is drawn from that item (context.item), only for the items of the plugin's own connector.

const uiTimeout = 20 * time.Second

// UIPlace is one thing a plugin draws, as the apps list it.
type UIPlace struct {
	Extension     string `json:"plugin"`
	ExtensionName string `json:"plugin_name,omitempty"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Placement     string `json:"placement"`
	Size          string `json:"size,omitempty"`
	// Icon: a nav_page's (plugin.UIIcons); Opens: the page an overview_card opens; Badge: on a nav_page's entry;
	// Connector: the connector whose items an inbox_item draws.
	Icon      string           `json:"icon,omitempty"`
	Opens     string           `json:"opens,omitempty"`
	Badge     *extension.Badge `json:"badge,omitempty"`
	Connector string           `json:"connector,omitempty"`
}

// uiPlaces are the plugins' UI of placement (every slot's but launcher sections' when it is empty); for launcher
// sections, those whose hosts have the gateway at base.
func (ps *extensions) uiPlaces(placement, base string) []UIPlace {
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
			slot, known := extension.Slot(u.Placement)
			switch {
			case !known:
				continue
			case placement == "" && u.Placement == "launcher_section":
				continue
			case placement != "" && u.Placement != placement:
				continue
			case u.Placement == "launcher_section" && (host == "" || (len(u.Hosts) > 0 && !hostMatches(host, u.Hosts)) || !p.Record.Granted.LauncherKeys):
				continue
			case u.Placement == "inbox_item" && !ps.servesConnector(hp, u.Connector):
				continue
			}
			place := UIPlace{Extension: p.Record.ID, ExtensionName: p.Manifest.Name, ID: u.ID, Title: u.Title, Placement: u.Placement,
				Size: slot.Size, Icon: u.Icon, Opens: u.Opens, Connector: u.Connector}
			if slot.Badge {
				if b, ok := ps.badges.Load(p.Record.ID + "/" + u.ID); ok {
					badge := b.(extension.Badge)
					place.Badge = &badge
				}
			}
			out = append(out, place)
		}
	}
	return out
}

// servesConnector: the connector registered as id is hp's own (the plugin brought it, or took it over from the hub).
func (ps *extensions) servesConnector(hp *hubExtension, id string) bool {
	if ps.h == nil || ps.h.connectors == nil || id == "" {
		return false
	}
	c := ps.h.connectors.Get(id)
	return c != nil && slices.ContainsFunc(hp.conns, func(own *proc.Conn) bool { return connector.Connector(own) == c })
}

// uiItem is inbox item id as an inbox_item is drawn for it, and the connector it came from (its source). The item is
// read as its JSON, not its fields: meta and the source blocks go to the plugin as they are stored.
func (h *Hub) uiItem(id string) (*extension.UIInboxItem, string, bool) {
	h.inbox.mu.Lock()
	it := h.inbox.items[id]
	var raw []byte
	if it != nil {
		raw, _ = json.Marshal(it)
	}
	h.inbox.mu.Unlock()
	if it == nil {
		return nil, "", false
	}
	return uiItemOf(raw)
}

// uiItemOf reads an inbox item's JSON as an inbox_item gets it, and its source.
func uiItemOf(raw []byte) (*extension.UIInboxItem, string, bool) {
	var row struct {
		ID      string          `json:"id"`
		Source  string          `json:"source"`
		Title   string          `json:"title"`
		Summary string          `json:"summary"`
		Tier    string          `json:"tier"`
		Reason  string          `json:"reason"`
		DoneAt  json.RawMessage `json:"done_at"`
		Meta    json.RawMessage `json:"meta"`
		// Count and StartedAt: for an event, the hub keeps both itself (inbox_group.go).
		Count     int       `json:"count"`
		StartedAt time.Time `json:"started_at"`
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil || json.Unmarshal(raw, &all) != nil {
		return nil, "", false
	}
	out := &extension.UIInboxItem{ID: row.ID, Title: row.Title, Summary: row.Summary, Tier: row.Tier, Reason: row.Reason,
		Done: len(row.DoneAt) > 0 && string(row.DoneAt) != "null", Meta: row.Meta, Count: row.Count, StartedAt: row.StartedAt}
	for _, k := range uiItemBlocks {
		if b, ok := all[k]; ok && string(b) != "null" {
			if out.Extra == nil {
				out.Extra = map[string]json.RawMessage{}
			}
			out.Extra[k] = b
		}
	}
	return out, row.Source, true
}

// uiItemBlocks are an inbox item's source blocks an inbox_item gets as extra.
var uiItemBlocks = []string{"lark", "doc", "moment", "mail", "connector"}

// uiRequest is what the apps send: render (no action) or an action, with where it is drawn, for whom, and the
// plugin's state.
type uiRequest struct {
	Context struct {
		Launcher string `json:"launcher,omitempty"`
		Machine  string `json:"machine,omitempty"`
		// Item is the inbox item an inbox_item is drawn for.
		Item     string `json:"item,omitempty"`
		TZ       string `json:"tz,omitempty"`
		Locale   string `json:"locale,omitempty"`
		Platform string `json:"platform,omitempty"`
		Size     string `json:"size,omitempty"`
		// UIVersion is the UI version the app draws: 1 (the first eight items) when it says nothing.
		UIVersion int `json:"ui_version,omitempty"`
	} `json:"context"`
	State  string            `json:"state,omitempty"`
	Action string            `json:"action,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}([-_][A-Za-z0-9]{1,8}){0,4}$`)

// uiParams are what the plugin is told of a request to draw item: the app's context held to what is known (a time
// zone that loads, the user's when the app says none; a locale that looks like one; a known platform and size).
func (h *Hub) uiParams(item extension.UIItem, slot extension.UISlot, in uiRequest) extension.UIParams {
	c := in.Context
	out := extension.UIParams{Contribution: item.ID, State: in.State, Placement: item.Placement, Size: slot.Size,
		UIVersion: min(max(c.UIVersion, 1), extension.UIVersion)}
	if c.Size == extension.SizeCompact || c.Size == extension.SizeFull {
		out.Size = c.Size
	}
	if _, err := time.LoadLocation(c.TZ); c.TZ != "" && len(c.TZ) <= 64 && err == nil {
		out.TZ = c.TZ
	} else {
		out.TZ = h.userTZName()
	}
	if len(c.Locale) <= 35 && localePattern.MatchString(c.Locale) {
		out.Locale = c.Locale
	}
	if slices.Contains(extension.UIPlatforms, c.Platform) {
		out.Platform = c.Platform
	}
	if in.Action != "" {
		out.Action, out.Values = in.Action, in.Values
	}
	return out
}

// uiCall asks plugin id's ui contribution to render or act.
func (h *Hub) uiCall(ctx context.Context, id, uiID string, in uiRequest) (extension.ActionResult, error) {
	var res extension.ActionResult
	p := h.extensions.reg.Get(id)
	hp := h.extensions.get(id)
	if p == nil || !p.Record.Enabled || hp == nil || hp.proc == nil {
		return res, errNoExtension
	}
	var item *extension.UIItem
	for _, u := range p.Manifest.UIs() {
		if u.ID == uiID {
			u := u
			item = &u
		}
	}
	if item == nil {
		return res, fmt.Errorf("%s 没有叫 %s 的界面", id, uiID)
	}
	slot, known := extension.Slot(item.Placement)
	if !known {
		return res, fmt.Errorf("这个 hub 没有 %s 这个位置", item.Placement)
	}
	params := h.uiParams(*item, slot, in)
	switch item.Placement {
	case "launcher_section":
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
		if err != nil || (len(item.Hosts) > 0 && !hostMatches(root.Hostname(), item.Hosts)) {
			return res, errors.New("这个启动器的网关不是这个插件的")
		}
		var b [12]byte
		_, _ = rand.Read(b[:])
		qid := hex.EncodeToString(b[:])
		quotaQueries.Store(qid, &quotaQuery{extension: id, send: gw.send})
		defer quotaQueries.Delete(qid)
		params.Launcher = &extension.UILauncher{Name: in.Context.Launcher, BaseURL: root.String(), QueryID: qid}
	case "machine_section":
		m := h.machineByID(in.Context.Machine)
		if m == nil && in.Context.Machine != "" {
			m = h.machineByName(in.Context.Machine)
		}
		if m == nil {
			return res, errNoMachine
		}
		s := m.snapshot()
		params.Machine = &extension.UIMachine{ID: s.ID, Name: s.Name, OS: s.OS, Online: s.Online}
	case "inbox_item":
		if in.Context.Item == "" {
			return res, errNoItem
		}
		if !h.extensions.servesConnector(hp, item.Connector) {
			return res, errNotServed
		}
		it, source, ok := h.uiItem(in.Context.Item)
		if !ok {
			return res, errNoItem
		}
		if source != item.Connector {
			return res, errNotItsItem
		}
		params.Item = it
	}
	method := "ui.render"
	if in.Action != "" {
		method = "ui.action"
	}
	cctx, cancel := context.WithTimeout(ctx, uiTimeout)
	defer cancel()
	var out json.RawMessage
	if err := hp.proc.Call(cctx, method, params, &out); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			if re.Code == extension.CodeUnsupported {
				return res, errUnsupportedGateway
			}
			return res, errors.New(re.Message)
		}
		return res, err
	}
	fit := func(v *extension.View) error {
		dropped, err := extension.Fit(v, slot, params.UIVersion)
		if len(dropped) > 0 && params.UIVersion == extension.UIVersion {
			h.extensions.logf("plugin %s: ui %s drew items this hub does not know (%s): left out", id, uiID, strings.Join(dropped, ", "))
		}
		if err != nil {
			return fmt.Errorf("插件画的界面这个 hub 画不了：%v", err)
		}
		return nil
	}
	if method == "ui.render" {
		var v extension.View
		if err := json.Unmarshal(out, &v); err != nil {
			return res, fmt.Errorf("插件画的界面这个 hub 画不了：%v", err)
		}
		if err := fit(&v); err != nil {
			return res, err
		}
		res.View = &v
		return res, nil
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return res, err
	}
	if res.View != nil {
		if err := fit(res.View); err != nil {
			return extension.ActionResult{}, err
		}
	}
	if err := checkOpenURL(*item, in.Context.Item, res.OpenURL); err != nil {
		return extension.ActionResult{}, err
	}
	return res, nil
}

// checkOpenURL holds what an action opens to https; an inbox_item may also open its item's own source page
// (plugin.InboxSourceURL), that item's only.
func checkOpenURL(ui extension.UIItem, itemID, raw string) error {
	if raw == "" {
		return nil
	}
	if ui.Placement == "inbox_item" && itemID != "" && raw == extension.InboxSourceURL(itemID) {
		return nil
	}
	if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || u.Host == "" {
		if strings.HasPrefix(raw, "persona:") {
			return errors.New("插件要打开的 persona: 链接不是这一条的来源（只有收件箱条目的界面能开 persona://inbox/<这一条>/source）")
		}
		return errors.New("插件要打开的不是 https 链接")
	}
	return nil
}

var (
	errNoExtension = errors.New("no such plugin")
	// errUnsupportedGateway: a launcher section without hosts whose plugin says the launcher's gateway is not its own;
	// the apps leave the section out.
	errUnsupportedGateway = errors.New("not this plugin's gateway")
	// errNoMachine: a machine section for a machine the hub does not have.
	errNoMachine = errors.New("没有这台机器")
	// errNoItem: an inbox item's page without the item, or for one the inbox does not have.
	errNoItem = errors.New("收件箱里没有这一条")
	// errNotItsItem: an inbox item's page for an item of another source than its connector.
	errNotItsItem = errors.New("这一条不是这个插件的连接器收进来的")
	// errNotServed: an inbox item's page whose connector is not the plugin's now (not registered, or another's); the
	// apps draw the item's own detail.
	errNotServed = errors.New("这个插件现在不管这个连接器")
)

func (h *Hub) extensionUIRoutes(api *http.ServeMux) {
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
		writeJSON(w, map[string]any{"ui": h.extensions.uiPlaces(placement, base)})
	})
	api.HandleFunc("POST /api/v1/plugins/{id}/-/ui/{ui}", func(w http.ResponseWriter, r *http.Request) {
		if principalOf(r.Context()).Role != roleFull {
			writeErr(w, http.StatusForbidden, errors.New("this device can only read and answer"))
			return
		}
		var in uiRequest
		if !readExtensionJSON(w, r, &in) {
			return
		}
		res, err := h.uiCall(r.Context(), r.PathValue("id"), r.PathValue("ui"), in)
		switch {
		case errors.Is(err, errNoExtension), errors.Is(err, errNoMachine), errors.Is(err, errNoItem):
			writeErrCode(w, http.StatusNotFound, "not_found", err)
		case errors.Is(err, errUnsupportedGateway), errors.Is(err, errNotServed):
			writeErrCode(w, http.StatusNotFound, "unsupported", err)
		case errors.Is(err, errNotItsItem):
			writeErrCode(w, http.StatusForbidden, "forbidden", err)
		case errors.Is(err, errOffline):
			writeErrCode(w, http.StatusServiceUnavailable, "offline", err)
		case err != nil:
			writeErrCode(w, http.StatusBadGateway, "plugin_failed", err)
		default:
			writeJSON(w, res)
		}
	})
}

// setBadge takes a plugin's ui.badge: the badge on one of its nav_pages' entries, told to the apps (plugin_ui).
func (ps *extensions) setBadge(id string, params json.RawMessage) {
	p := ps.reg.Get(id)
	var ev extension.BadgeEvent
	if p == nil || json.Unmarshal(params, &ev) != nil {
		return
	}
	i := slices.IndexFunc(p.Manifest.UIs(), func(u extension.UIItem) bool { return u.ID == ev.Contribution })
	if i < 0 {
		ps.logf("plugin %s: ui.badge for %q, which it does not have: dropped", id, ev.Contribution)
		return
	}
	if slot, _ := extension.Slot(p.Manifest.UIs()[i].Placement); !slot.Badge {
		ps.logf("plugin %s: ui.badge for %s, which has no entry to badge: dropped", id, ev.Contribution)
		return
	}
	if err := extension.CheckBadge(ev.Text, ev.Tone); err != nil {
		ps.logf("plugin %s: ui.badge for %s: %v: dropped", id, ev.Contribution, err)
		return
	}
	key := id + "/" + ev.Contribution
	var badge *extension.Badge
	if ev.Text == "" {
		if _, had := ps.badges.LoadAndDelete(key); !had {
			return
		}
	} else {
		b := extension.Badge{Text: ev.Text, Tone: ev.Tone}
		if old, had := ps.badges.Swap(key, b); had && old.(extension.Badge) == b {
			return
		}
		badge = &b
	}
	if ps.h != nil {
		ps.h.broadcast(map[string]any{"type": "plugin_ui", "plugin": id, "id": ev.Contribution, "badge": badge})
	}
}

// clearBadges forgets a plugin's badges: it (re)starts, and says them again.
func (ps *extensions) clearBadges(id string) {
	ps.badges.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), id+"/") {
			ps.badges.Delete(k)
		}
		return true
	})
}

// emit tells the plugins subscribed to topic (running ones: events need on_startup) about it; never waits.
func (ps *extensions) emit(topic string, payload map[string]any) {
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
