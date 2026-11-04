package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Places outside the app (docs/extensions.md「系统级位置」): an extension's widgets (the ui slot widget: a home screen
// widget, a Lock Screen or watch face complication) and its shortcuts (actions Shortcuts and Siri run). The apps list
// them, draw a widget for a family (held to what the family draws, kept until it is due again) and run a shortcut;
// each device says which widgets it has, so an extension's ui.invalidate wakes the devices that show it.

// widgetPushEvery is how often one device is woken for one widget at most: iOS budgets background pushes and widget
// reloads, and a widget a minute old is fine.
var widgetPushEvery = 15 * time.Minute

// deviceWidgetsKey is the setting keeping the widgets each device has.
const deviceWidgetsKey = "extension_widgets"

// deviceWidgetsMost is how many widgets one device may report.
const deviceWidgetsMost = 64

// WidgetPlace is one widget an extension draws, as the apps list it.
type WidgetPlace = wire.WidgetPlace

// ShortcutPlace is one shortcut of an extension, as the apps list it.
type ShortcutPlace = wire.ShortcutPlace

// widgets keeps what the hub knows of widgets: views drawn and not yet due, the widgets each device has, when each was
// last woken for one.
type widgets struct {
	mu      sync.Mutex
	views   map[string]widgetView          // widgetCacheKey → the view
	devices map[string][]wire.DeviceWidget // device id → its widgets
	loaded  bool                           // devices read from the store
	woken   map[string]time.Time           // device/extension/ui → when it was last woken for it
}

type widgetView struct {
	view    extension.View
	expires time.Time
}

// widgetCacheKey is a view's key: whose widget, at which family, for which context.
func widgetCacheKey(ext, ui string, p extension.UIParams) string {
	return strings.Join([]string{ext, ui, p.Size, p.TZ, p.Locale, p.Platform, strconv.Itoa(p.UIVersion)}, "\x00")
}

// widgetPlaces are the widgets of the extensions that are on and running.
func (ps *extensions) widgetPlaces() []WidgetPlace {
	out := []WidgetPlace{}
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		if hp := ps.get(p.Record.ID); hp == nil || hp.proc == nil {
			continue
		}
		for _, u := range p.Manifest.UIs() {
			if u.Placement != "widget" {
				continue
			}
			out = append(out, WidgetPlace{Extension: p.Record.ID, ExtensionName: p.Manifest.Name, ID: u.ID, Title: u.Title,
				Families: u.Families, RefreshMinutes: int(u.Refresh() / time.Minute), Opens: u.Opens})
		}
	}
	return out
}

// shortcutPlaces are the shortcuts of the extensions that are on and running.
func (ps *extensions) shortcutPlaces() []ShortcutPlace {
	out := []ShortcutPlace{}
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		if hp := ps.get(p.Record.ID); hp == nil || hp.proc == nil {
			continue
		}
		for _, s := range p.Manifest.Shortcuts() {
			out = append(out, ShortcutPlace{Extension: p.Record.ID, ExtensionName: p.Manifest.Name, ID: s.ID, Title: s.Title,
				Description: s.Description, Params: s.Params, UI: s.UI})
		}
	}
	return out
}

var (
	errNotWidget   = errors.New("这不是一个小组件")
	errNoShortcut  = errors.New("这个扩展没有这个快捷指令")
	errBadFamily   = errors.New("这个小组件不画这个尺寸")
	errShortcutArg = errors.New("快捷指令的参数不对")
	// errUseWidget: a widget asked for as a page (…/-/ui/{ui}); widgets are drawn with …/-/widget/{ui}.
	errUseWidget = errors.New("小组件用 /api/v1/extensions/{id}/-/widget/{ui} 画")
)

// widgetCall draws extension id's widget ui at the family in.Context.Size: the view kept from before while it is not
// due, else the extension's ui.render held to the family (FitWidget), kept until the widget's refresh is due.
func (h *Hub) widgetCall(ctx context.Context, id, uiID string, in uiRequest) (wire.WidgetAnswer, error) {
	var out wire.WidgetAnswer
	p := h.extensions.reg.Get(id)
	hp := h.extensions.get(id)
	if p == nil || !p.Record.Enabled || hp == nil || hp.proc == nil {
		return out, errNoExtension
	}
	i := slices.IndexFunc(p.Manifest.UIs(), func(u extension.UIItem) bool { return u.ID == uiID })
	if i < 0 {
		return out, errNoExtension
	}
	item := p.Manifest.UIs()[i]
	if item.Placement != "widget" {
		return out, errNotWidget
	}
	family := in.Context.Size
	if !slices.Contains(item.Families, family) {
		return out, fmt.Errorf("%w：%s 只画 %s", errBadFamily, item.Title, strings.Join(item.Families, "、"))
	}
	slot, _ := extension.Slot("widget")
	params := h.uiParams(item, slot, in)
	params.Size, params.State, params.Action, params.Values = family, "", "", nil
	key := widgetCacheKey(id, uiID, params)
	w := &h.extensions.widgets
	now := time.Now()
	w.mu.Lock()
	if v, ok := w.views[key]; ok && now.Before(v.expires) {
		w.mu.Unlock()
		return wire.WidgetAnswer{View: v.view, ExpiresAt: v.expires}, nil
	}
	w.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, uiTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := hp.proc.Call(cctx, "ui.render", params, &raw); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return out, errors.New(re.Message)
		}
		return out, err
	}
	var v extension.View
	if err := json.Unmarshal(raw, &v); err != nil {
		return out, fmt.Errorf("扩展画的小组件这个 hub 画不了：%v", err)
	}
	dropped, err := extension.FitWidget(&v, family, params.UIVersion)
	if err != nil {
		return out, fmt.Errorf("扩展画的小组件这个 hub 画不了：%v", err)
	}
	if len(dropped) > 0 {
		h.extensions.logf("extension %s: widget %s (%s) drew what a widget does not (%s): left out", id, uiID, family, strings.Join(dropped, ", "))
	}
	expires := now.Add(item.Refresh()).UTC().Truncate(time.Second)
	w.mu.Lock()
	if w.views == nil {
		w.views = map[string]widgetView{}
	}
	for k, old := range w.views { // drop the due ones as new ones come: the map stays the size of the widgets on screens
		if !now.Before(old.expires) {
			delete(w.views, k)
		}
	}
	w.views[key] = widgetView{view: v, expires: expires}
	w.mu.Unlock()
	return wire.WidgetAnswer{View: v, ExpiresAt: expires}, nil
}

// shortcutCall runs extension id's shortcut sid with the values: they are checked against its params (declared names
// only, the required ones there, numbers that parse, toggles true or false, one of a choice's values), then the
// extension's shortcut.run answers a sentence and, maybe, a view held to a large widget's subset.
func (h *Hub) shortcutCall(ctx context.Context, id, sid string, in wire.ShortcutRequest) (wire.ShortcutResult, error) {
	var out wire.ShortcutResult
	p := h.extensions.reg.Get(id)
	hp := h.extensions.get(id)
	if p == nil || !p.Record.Enabled || hp == nil || hp.proc == nil {
		return out, errNoExtension
	}
	i := slices.IndexFunc(p.Manifest.Shortcuts(), func(s extension.ShortcutItem) bool { return s.ID == sid })
	if i < 0 {
		return out, errNoShortcut
	}
	sc := p.Manifest.Shortcuts()[i]
	values := map[string]string{}
	for name, v := range in.Values {
		j := slices.IndexFunc(sc.Params, func(p extension.ShortcutParam) bool { return p.Name == name })
		if j < 0 {
			return out, fmt.Errorf("%w：没有参数 %s", errShortcutArg, name)
		}
		if err := checkShortcutValue(sc.Params[j], v); err != nil {
			return out, fmt.Errorf("%w：%s %v", errShortcutArg, sc.Params[j].Title, err)
		}
		values[name] = v
	}
	for _, p := range sc.Params {
		if p.Required && values[p.Name] == "" {
			return out, fmt.Errorf("%w：缺 %s", errShortcutArg, p.Title)
		}
	}
	var req uiRequest
	if in.Context != nil {
		c := in.Context
		req.Context.TZ, req.Context.Locale, req.Context.Platform, req.Context.UIVersion = c.TZ, c.Locale, c.Platform, c.UIVersion
	}
	slot, _ := extension.Slot("widget")
	ctxParams := h.uiParams(extension.UIItem{ID: sid, Placement: "widget"}, slot, req)
	params := wire.ShortcutRunParams{Contribution: sid, Values: values, UIVersion: ctxParams.UIVersion, TZ: ctxParams.TZ,
		Locale: ctxParams.Locale, Platform: ctxParams.Platform}
	cctx, cancel := context.WithTimeout(ctx, uiTimeout)
	defer cancel()
	if err := hp.proc.Call(cctx, "shortcut.run", params, &out); err != nil {
		var re *rpc.Error
		if errors.As(err, &re) {
			return out, errors.New(re.Message)
		}
		return out, err
	}
	out.Text = clipRunes(out.Text, extension.StringMost)
	if out.View != nil {
		if _, err := extension.FitWidget(out.View, "large", params.UIVersion); err != nil {
			return wire.ShortcutResult{}, fmt.Errorf("扩展回的结果这个 hub 画不了：%v", err)
		}
	}
	return out, nil
}

// checkShortcutValue holds a value to its param's kind.
func checkShortcutValue(p extension.ShortcutParam, v string) error {
	switch p.Kind {
	case "number":
		if _, err := strconv.ParseFloat(v, 64); err != nil && v != "" {
			return errors.New("要一个数")
		}
	case "toggle":
		if v != "true" && v != "false" && v != "" {
			return errors.New("是 true 或 false")
		}
	case "choice":
		if v != "" && !slices.ContainsFunc(p.Choices, func(c wire.Choice) bool { return c.Value == v }) {
			return errors.New("不是它的选项")
		}
	}
	if len(v) > extension.StringMost {
		return errors.New("太长")
	}
	return nil
}

// deviceWidgets are the widgets each device has (device id → its widgets), as each last said; the caller holds no
// lock and reads the map under widgets.mu.
func (h *Hub) deviceWidgets(ctx context.Context) map[string][]wire.DeviceWidget {
	w := &h.extensions.widgets
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.loaded && h.store != nil {
		var stored map[string][]wire.DeviceWidget
		if _, err := h.store.Setting(ctx, deviceWidgetsKey, &stored); err == nil {
			w.devices, w.loaded = stored, true
		}
	}
	if w.devices == nil {
		w.devices = map[string][]wire.DeviceWidget{}
	}
	return w.devices
}

// setDeviceWidgets keeps the widgets device id says it has (none forgets it).
func (h *Hub) setDeviceWidgets(ctx context.Context, id string, list []wire.DeviceWidget) error {
	all := h.deviceWidgets(ctx)
	w := &h.extensions.widgets
	w.mu.Lock()
	defer w.mu.Unlock() // through the write: two devices saying theirs at once are stored in the order they were taken
	if len(list) == 0 {
		delete(all, id)
	} else {
		all[id] = list
	}
	if h.store == nil {
		return nil
	}
	return h.store.PutSetting(ctx, deviceWidgetsKey, all)
}

// widgetsChanged takes what an extension said changed (ui.invalidate: its ui ids, "" for all of them): the views kept
// of them are dropped, and each device showing one of them is woken (a background push with extension_widget), once
// in widgetPushEvery per widget.
func (h *Hub) widgetsChanged(id string, ids map[string]bool) {
	if h.extensions == nil {
		return
	}
	p := h.extensions.reg.Get(id)
	if p == nil {
		return
	}
	changed := map[string]bool{}
	for _, u := range p.Manifest.UIs() {
		if u.Placement == "widget" && (ids[""] || ids[u.ID]) {
			changed[u.ID] = true
		}
	}
	if len(changed) == 0 {
		return
	}
	w := &h.extensions.widgets
	w.mu.Lock()
	for k := range w.views {
		parts := strings.SplitN(k, "\x00", 3)
		if parts[0] == id && changed[parts[1]] {
			delete(w.views, k)
		}
	}
	w.mu.Unlock()
	devices := h.deviceWidgets(context.Background())
	now := time.Now()
	w.mu.Lock()
	if w.woken == nil {
		w.woken = map[string]time.Time{}
	}
	wake := map[string][]string{} // device → widgets to wake it for
	for dev, list := range devices {
		for _, dw := range list {
			k := dev + "/" + dw.Extension + "/" + dw.ID
			if dw.Extension != id || !changed[dw.ID] || slices.Contains(wake[dev], dw.ID) || now.Sub(w.woken[k]) < widgetPushEvery {
				continue
			}
			w.woken[k] = now
			wake[dev] = append(wake[dev], dw.ID)
		}
	}
	w.mu.Unlock()
	if h.push == nil {
		return
	}
	for dev, uis := range wake {
		for _, ui := range uis {
			dev := dev
			h.push.sendAs(map[string]any{"aps": map[string]any{"content-available": 1},
				"extension_widget": wire.WidgetPush{Extension: id, ID: ui}}, "", "background", "5",
				func(d Device) bool { return d.DeviceID == dev })
		}
	}
}

func (h *Hub) extensionSystemRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/extensions/-/widgets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, wire.WidgetPlaces{Widgets: h.extensions.widgetPlaces()})
	})
	api.HandleFunc("POST /api/v1/extensions/{id}/-/widget/{ui}", func(w http.ResponseWriter, r *http.Request) {
		var in uiRequest
		if !readExtensionJSON(w, r, &in) {
			return
		}
		res, err := h.widgetCall(r.Context(), r.PathValue("id"), r.PathValue("ui"), in)
		switch {
		case errors.Is(err, errNoExtension):
			writeErrCode(w, http.StatusNotFound, "not_found", err)
		case errors.Is(err, errNotWidget), errors.Is(err, errBadFamily):
			writeErrCode(w, http.StatusBadRequest, "bad_request", err)
		case err != nil:
			writeErrCode(w, http.StatusBadGateway, "plugin_failed", err)
		default:
			writeJSON(w, res)
		}
	})
	api.HandleFunc("GET /api/v1/extensions/-/shortcuts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, wire.ShortcutPlaces{Shortcuts: h.extensions.shortcutPlaces()})
	})
	api.HandleFunc("POST /api/v1/extensions/{id}/-/shortcut/{sid}", func(w http.ResponseWriter, r *http.Request) {
		var in wire.ShortcutRequest
		if !readExtensionJSON(w, r, &in) {
			return
		}
		res, err := h.shortcutCall(r.Context(), r.PathValue("id"), r.PathValue("sid"), in)
		switch {
		case errors.Is(err, errNoExtension), errors.Is(err, errNoShortcut):
			writeErrCode(w, http.StatusNotFound, "not_found", err)
		case errors.Is(err, errShortcutArg):
			writeErrCode(w, http.StatusBadRequest, "bad_request", err)
		case err != nil:
			writeErrCode(w, http.StatusBadGateway, "plugin_failed", err)
		default:
			writeJSON(w, res)
		}
	})
	// Which widgets this device has on its screens (the app says it as they change): an extension's ui.invalidate
	// wakes it for those only.
	api.HandleFunc("PUT /api/v1/devices/{id}/widgets", func(w http.ResponseWriter, r *http.Request) {
		var in wire.DeviceWidgets
		if !readExtensionJSON(w, r, &in) {
			return
		}
		if len(in.Widgets) > deviceWidgetsMost {
			writeErrCode(w, http.StatusBadRequest, "bad_request", fmt.Errorf("at most %d widgets", deviceWidgetsMost))
			return
		}
		var list []wire.DeviceWidget
		for _, dw := range in.Widgets {
			if dw.Extension == "" || dw.ID == "" || !slices.Contains(extension.WidgetFamilies, dw.Family) {
				writeErrCode(w, http.StatusBadRequest, "bad_request", errors.New("a widget is {extension, id, family}"))
				return
			}
			if !slices.ContainsFunc(list, func(o wire.DeviceWidget) bool { return o == dw }) {
				list = append(list, dw)
			}
		}
		if err := h.setDeviceWidgets(r.Context(), r.PathValue("id"), list); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if list == nil {
			list = []wire.DeviceWidget{}
		}
		writeJSON(w, wire.DeviceWidgets{Widgets: list})
	})
}
