package hub

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// serveUIPluginForTest draws a page (a metric, a field, a button), a launcher section that reads the gateway through
// the hub, and a page the apps could not draw; it counts the agent.finished events it is told.
func serveUIExtensionForTest() {
	var hub *pserve.Hub
	var finished atomic.Int32
	ui := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		var p struct {
			Contribution string            `json:"contribution"`
			Action       string            `json:"action"`
			Values       map[string]string `json:"values"`
			Launcher     struct {
				QueryID string `json:"query_id"`
			} `json:"launcher"`
		}
		_ = json.Unmarshal(params, &p)
		var c extension.UIParams
		_ = json.Unmarshal(params, &c)
		switch {
		case method == "action" && p.Action == "save":
			return map[string]any{"toast": "存好了：" + p.Values["note"]}, nil
		case method == "action" && p.Action == "badge":
			_ = hub.Notify("ui.badge", map[string]any{"contribution": "nav", "text": p.Values["n"], "tone": "danger"})
			return map[string]any{"toast": "ok"}, nil
		case p.Contribution == "nav":
			said := fmt.Sprintf("%s %s %s %s %s v%d", c.Placement, c.Size, c.TZ, c.Locale, c.Platform, c.UIVersion)
			return map[string]any{"refresh_after": 5, "sections": []any{map[string]any{"items": []any{
				map[string]any{"type": "text", "text": said},
				map[string]any{"type": "chart", "kind": "bar", "unit": "tokens", "series": []any{
					map[string]any{"name": "输入", "points": []any{map[string]any{"label": "周一", "value": 1}, map[string]any{"label": "周二", "value": 2}}},
					map[string]any{"name": "输出", "points": []any{map[string]any{"label": "周一", "value": 3}, map[string]any{"label": "周二", "value": 4}}}}},
				map[string]any{"type": "stat_grid", "stats": []any{map[string]any{"label": "今天", "value": "1.2M"}, map[string]any{"label": "7 天", "value": "8M", "tone": "positive"}}},
				map[string]any{"type": "sparkle", "text": "from a newer hub"}}}}}, nil
		case p.Contribution == "card":
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "metric", "label": "今天", "value": "1.2M"}}}}}, nil
		case p.Contribution == "bigcard":
			var items []any
			for range extension.CompactItems + 1 {
				items = append(items, map[string]any{"type": "divider"})
			}
			return map[string]any{"sections": []any{map[string]any{"items": items}}}, nil
		case p.Contribution == "box":
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "text", "text": c.Machine.Name + " " + c.Machine.ID}}}}}, nil
		case p.Contribution == "panel":
			var r quota.Response
			if err := hub.Call(ctx, "gateway.http", map[string]any{"query_id": p.Launcher.QueryID, "url": "/panel",
				"headers": map[string]string{"Authorization": "Bearer " + quota.KeyPlaceholder}}, &r); err != nil {
				return nil, err
			}
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "text", "text": r.Body}}}}}, nil
		case p.Contribution == "any":
			return nil, &rpc.Error{Code: extension.CodeUnsupported, Message: "not mine"}
		case p.Contribution == "broken":
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "link", "title": "x", "url": "http://example.com"}}}}}, nil
		}
		return map[string]any{"title": "示例", "state": "page=1", "sections": []any{map[string]any{"title": "概况", "items": []any{
			map[string]any{"type": "metric", "label": "完成的 Agent", "value": fmt.Sprint(finished.Load()), "tone": "positive"},
			map[string]any{"type": "field", "name": "note", "label": "备注", "kind": "text"},
			map[string]any{"type": "button", "title": "保存", "action": map[string]any{"id": "save", "submit": true}}}}}}, nil
	}
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-ui", Version: cmp.Or(os.Getenv("EXAMPLE_UI_VERSION"), "1.0.0"),
		Init: func(_ context.Context, _ extension.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			hub = h
			return nil, nil
		},
		Namespaces: map[string]pserve.Handler{
			"ui": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
				if method != "render" && method != "action" {
					return nil, pserve.UnknownMethod(method)
				}
				return ui(ctx, method, params)
			},
			"event": func(_ context.Context, method string, _ json.RawMessage) (any, error) {
				if method == "agent.finished" {
					finished.Add(1)
				}
				return nil, nil
			}}})
}

func uiPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "ui"), []byte("#!/bin/sh\nJARVIS_TEST_PLUGIN=ui exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-ui", "name": "Example UI", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/ui"}}, "activation_events": ["on_startup"],
		"contributes": {
			"ui": [{"id": "home", "title": "概况", "placement": "plugin_page"},
			       {"id": "broken", "title": "坏的", "placement": "plugin_page"},
			       {"id": "panel", "title": "网关面板", "placement": "launcher_section", "hosts": ["127.0.0.1"]},
			       {"id": "any", "title": "任意网关", "placement": "launcher_section"},
			       {"id": "nav", "title": "用量", "placement": "nav_page", "icon": "chart"},
			       {"id": "card", "title": "用量", "placement": "overview_card", "opens": "nav"},
			       {"id": "bigcard", "title": "太大的卡片", "placement": "overview_card"},
			       {"id": "box", "title": "这台机器", "placement": "machine_section"}],
			"events": [{"id": "agents", "subscribe": ["agent.finished"]}]},
		"permissions": {"launcher_keys": true}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A plugin's declarative UI: listed where it goes, drawn by the plugin and held to what the apps draw, its actions
// answered; a launcher section reaches its gateway with the key the plugin never sees; events reach it.
func TestExtensionUIAndEvents(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	const key = "sk-example-panel-key"
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "auth=%s", r.Header.Get("Authorization"))
	}))
	defer gw.Close()
	if code, out := call(t, mux, "POST", "/api/v1/launchers", master,
		fmt.Sprintf(`{"name": "panellauncher", "base_url": %q, "model": "m", "api_key": %q}`, gw.URL, key)); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+uiPackage(t)+`"}`)
	if !strings.Contains(fmt.Sprint(pv["permissions"]), "收到 hub 的事件") {
		t.Fatalf("the events it is told are shown before installing: %v", pv["permissions"])
	}
	call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	waitLong(t, "the plugin up", func() bool {
		hp := h.extensions.get("example-ui")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	h.notifyDone("done", "mac", "Mac", "s1", "✅ 完成了", "", pushWants{Kind: "done"})
	_, detail := call(t, mux, "GET", "/api/v1/plugins/example-ui/-/", master, "")
	if ui, _ := detail["ui"].([]any); len(ui) != 3 {
		t.Fatalf("its pages (plugin_page and nav_page): %v", detail["ui"])
	}
	var view map[string]any
	waitLong(t, "the event to be counted", func() bool {
		_, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/home", master, `{}`)
		view, _ = out["view"].(map[string]any)
		return strings.Contains(fmt.Sprint(view), "value:1")
	})
	if view["state"] != "page=1" || view["title"] != "示例" {
		t.Fatalf("%v", view)
	}
	_, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/home", master, `{"action": "save", "values": {"note": "你好"}, "state": "page=1"}`)
	if out["toast"] != "存好了：你好" {
		t.Fatalf("%v", out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/broken", master, `{}`); code != 502 || !strings.Contains(fmt.Sprint(out["error"]), "画不了") {
		t.Fatalf("a view the apps cannot draw is refused: %d %v", code, out)
	}

	_, places := call(t, mux, "GET", "/api/v1/plugins/-/ui?placement=launcher_section&launcher=panellauncher", master, "")
	if list, _ := places["ui"].([]any); len(list) != 2 || fmt.Sprint(list) != fmt.Sprint([]any{list[0], list[1]}) || !strings.Contains(fmt.Sprint(list), "id:panel") || !strings.Contains(fmt.Sprint(list), "id:any") {
		t.Fatalf("the launcher's sections (by host, and one for any gateway): %v", places)
	}
	if code, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/any", master, `{"context": {"launcher": "panellauncher"}}`); code != 404 || out["code"] != "unsupported" {
		t.Fatalf("a section for any gateway, not this one's: %d %v", code, out)
	}
	_, out = call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/panel", master, `{"context": {"launcher": "panellauncher"}}`)
	text := fmt.Sprint(out["view"])
	if !strings.Contains(text, "auth=Bearer "+quota.KeyPlaceholder) || strings.Contains(text, key) {
		t.Fatalf("the gateway got the key and the plugin did not: %s", text)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/panel", master, `{}`); code == 200 {
		t.Fatal("a launcher section needs its launcher")
	}
}

// The slots outside a plugin's settings: listed with their size, icon, page to open and badge; drawn with the app's
// context; a View held to what the asking app draws and to a card's size; shown to the user before installing.
func TestExtensionUISlots(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	m := &Machine{hub: h, cfg: MachineConfig{ID: "box1", Name: "Studio"}}
	h.machines, h.byID["box1"] = append(h.machines, m), m
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+uiPackage(t)+`"}`)
	shown := fmt.Sprint(pv["permissions"])
	if !strings.Contains(shown, "在 App 里显示自己的界面") || !strings.Contains(shown, "主导航上的入口：用量") ||
		!strings.Contains(shown, "总览上的卡片：用量、太大的卡片") || !strings.Contains(shown, "机器详情页里的一节") {
		t.Fatalf("where it draws is shown before installing: %v", shown)
	}
	call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	waitLong(t, "the plugin up", func() bool {
		hp := h.extensions.get("example-ui")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	_, all := call(t, mux, "GET", "/api/v1/plugins/-/ui", master, "")
	list := fmt.Sprint(all["ui"])
	for _, want := range []string{"id:nav", "icon:chart", "size:full", "id:card", "opens:nav", "size:compact", "id:box", "plugin_name:Example UI", "id:home"} {
		if !strings.Contains(list, want) {
			t.Fatalf("every slot's but launchers' (%s): %s", want, list)
		}
	}
	if strings.Contains(list, "id:panel") || strings.Contains(list, "badge") {
		t.Fatalf("no launcher sections, no badge yet: %s", list)
	}
	_, nav := call(t, mux, "GET", "/api/v1/plugins/-/ui?placement=nav_page", master, "")
	if l, _ := nav["ui"].([]any); len(l) != 1 {
		t.Fatalf("one slot: %v", nav)
	}

	_, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/nav", master,
		`{"context": {"tz": "Asia/Tokyo", "locale": "zh-Hans-CN", "platform": "web", "ui_version": 2}}`)
	view := fmt.Sprint(out["view"])
	if !strings.Contains(view, "nav_page full Asia/Tokyo zh-Hans-CN web v2") || !strings.Contains(view, "type:chart") ||
		!strings.Contains(view, "type:stat_grid") || strings.Contains(view, "sparkle") || !strings.Contains(view, "refresh_after:30") {
		t.Fatalf("drawn with the app's context, a newer hub's item left out, refresh_after raised to its least: %s", view)
	}
	_, out = call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/nav", master,
		`{"context": {"tz": "Mars/Base", "locale": "<script>", "platform": "fridge"}}`)
	view = fmt.Sprint(out["view"])
	if !strings.Contains(view, "nav_page full") || !strings.Contains(view, " v1") || strings.Contains(view, "Mars") ||
		strings.Contains(view, "script") || strings.Contains(view, "fridge") || strings.Contains(view, "type:chart") || strings.Contains(view, "stat_grid") {
		t.Fatalf("an older app: version 1, what it cannot draw left out; context it made up dropped: %s", view)
	}

	_, out = call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/card", master, `{"context": {"ui_version": 2}}`)
	if !strings.Contains(fmt.Sprint(out["view"]), "1.2M") {
		t.Fatalf("the card: %v", out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/bigcard", master, `{"context": {"ui_version": 2}}`); code != 502 || !strings.Contains(fmt.Sprint(out["error"]), "card") {
		t.Fatalf("a card holds %d items: %d %v", extension.CompactItems, code, out)
	}

	if code, out := call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/box", master, `{}`); code != 404 {
		t.Fatalf("a machine section needs its machine: %d %v", code, out)
	}
	for _, which := range []string{"box1", "studio"} {
		_, out = call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/box", master, `{"context": {"machine": "`+which+`"}}`)
		if !strings.Contains(fmt.Sprint(out["view"]), "Studio box1") {
			t.Fatalf("the machine, by id or name (%s): %v", which, out)
		}
	}

	call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/nav", master, `{"action": "badge", "values": {"n": "12"}}`)
	waitLong(t, "the badge", func() bool {
		_, nav = call(t, mux, "GET", "/api/v1/plugins/-/ui?placement=nav_page", master, "")
		return strings.Contains(fmt.Sprint(nav["ui"]), "badge:map[text:12 tone:danger]")
	})
	call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/nav", master, `{"action": "badge", "values": {"n": "far too long a badge"}}`)
	call(t, mux, "POST", "/api/v1/plugins/example-ui/-/ui/nav", master, `{"action": "badge", "values": {"n": ""}}`)
	waitLong(t, "the badge cleared (a too long one dropped)", func() bool {
		_, nav = call(t, mux, "GET", "/api/v1/plugins/-/ui?placement=nav_page", master, "")
		return !strings.Contains(fmt.Sprint(nav["ui"]), "badge")
	})
}

func TestCheckView(t *testing.T) {
	ratio := 0.4
	ok := extension.View{Sections: []extension.Section{{Items: []extension.UIElem{
		{Type: "text", Text: "x"}, {Type: "progress", Label: "l", Ratio: &ratio, Tone: "caution"},
		{Type: "row", Title: "r", Badge: &extension.Badge{Text: "b", Tone: "danger"}, Action: &extension.UIAction{ID: "a"}},
		{Type: "link", Title: "l", URL: "https://example.com"}, {Type: "field", Name: "f", Label: "F", Kind: "choice", Choices: []extension.UIChoice{{Value: "1"}}},
		{Type: "divider"},
		{Type: "chart", Kind: "bar", Unit: "tokens", Stacked: true, Series: []extension.ChartSeries{
			{Name: "in", Tone: "positive", Points: []extension.ChartPoint{{Label: "一", Value: 1}, {Label: "二", Value: -2, Display: "-2"}}},
			{Name: "out", Points: []extension.ChartPoint{{Label: "一", Value: 3}, {Label: "二", Value: 4}}}}},
		{Type: "stat_grid", Stats: []extension.Stat{{Label: "a", Value: "1"}, {Label: "b", Value: "2", Detail: "+3%", Tone: "positive"}}},
		{Type: "segmented", Name: "range", Value: "7d", Action: &extension.UIAction{ID: "range"}, Choices: []extension.UIChoice{{Value: "1d", Title: "今天"}, {Value: "7d", Title: "7 天"}}}}}}}
	if err := extension.CheckView(&ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]extension.UIElem{
		"unknown type":   {Type: "html"},
		"tone":           {Type: "metric", Label: "a", Value: "b", Tone: "#ff0000"},
		"http link":      {Type: "link", Title: "x", URL: "http://example.com"},
		"secret value":   {Type: "field", Name: "s", Label: "S", Kind: "secret", Value: "leak"},
		"button":         {Type: "button", Title: "x"},
		"ratio":          {Type: "progress", Label: "x", Ratio: func() *float64 { v := 1.5; return &v }()},
		"long text":      {Type: "text", Text: strings.Repeat("长", extension.StringMost+1)},
		"empty choices":  {Type: "field", Name: "c", Label: "C", Kind: "choice"},
		"action without": {Type: "row", Title: "x", Action: &extension.UIAction{}},
		"chart kind":     {Type: "chart", Kind: "pie", Series: []extension.ChartSeries{{Points: []extension.ChartPoint{{Label: "a", Value: 1}}}}},
		"chart empty":    {Type: "chart", Kind: "bar"},
		"chart labels": {Type: "chart", Kind: "line", Series: []extension.ChartSeries{
			{Name: "a", Points: []extension.ChartPoint{{Label: "x", Value: 1}}}, {Name: "b", Points: []extension.ChartPoint{{Label: "y", Value: 1}}}}},
		"chart unnamed": {Type: "chart", Kind: "line", Series: []extension.ChartSeries{
			{Points: []extension.ChartPoint{{Label: "x", Value: 1}}}, {Points: []extension.ChartPoint{{Label: "x", Value: 1}}}}},
		"chart tone":    {Type: "chart", Kind: "bar", Series: []extension.ChartSeries{{Tone: "blue", Points: []extension.ChartPoint{{Label: "x"}}}}},
		"segment one":   {Type: "segmented", Name: "r", Action: &extension.UIAction{ID: "r"}, Choices: []extension.UIChoice{{Value: "a"}}, Value: "a"},
		"segment value": {Type: "segmented", Name: "r", Action: &extension.UIAction{ID: "r"}, Choices: []extension.UIChoice{{Value: "a"}, {Value: "b"}}, Value: "c"},
		"segment act":   {Type: "segmented", Name: "r", Choices: []extension.UIChoice{{Value: "a"}, {Value: "b"}}, Value: "a"},
		"stats empty":   {Type: "stat_grid"},
		"stats many":    {Type: "stat_grid", Stats: make([]extension.Stat, extension.StatsMost+1)},
	}
	for name, e := range bad {
		if err := extension.CheckView(&extension.View{Sections: []extension.Section{{Items: []extension.UIElem{e}}}}); err == nil {
			t.Errorf("%s: drawn", name)
		}
	}
}
