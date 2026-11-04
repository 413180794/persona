package hub

import (
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

	"github.com/nwverse/persona/server/internal/plugin"
	pserve "github.com/nwverse/persona/server/internal/plugin/serve"
	"github.com/nwverse/persona/server/internal/quota"
)

// serveUIPluginForTest draws a page (a metric, a field, a button), a launcher section that reads the gateway through
// the hub, and a page the apps could not draw; it counts the agent.finished events it is told.
func serveUIPluginForTest() {
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
		switch {
		case method == "action" && p.Action == "save":
			return map[string]any{"toast": "存好了：" + p.Values["note"]}, nil
		case p.Contribution == "panel":
			var r quota.Response
			if err := hub.Call(ctx, "gateway.http", map[string]any{"query_id": p.Launcher.QueryID, "url": "/panel",
				"headers": map[string]string{"Authorization": "Bearer " + quota.KeyPlaceholder}}, &r); err != nil {
				return nil, err
			}
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "text", "text": r.Body}}}}}, nil
		case p.Contribution == "broken":
			return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "iframe"}}}}}, nil
		}
		return map[string]any{"title": "示例", "state": "page=1", "sections": []any{map[string]any{"title": "概况", "items": []any{
			map[string]any{"type": "metric", "label": "完成的 Agent", "value": fmt.Sprint(finished.Load()), "tone": "positive"},
			map[string]any{"type": "field", "name": "note", "label": "备注", "kind": "text"},
			map[string]any{"type": "button", "title": "保存", "action": map[string]any{"id": "save", "submit": true}}}}}}, nil
	}
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-ui", Version: "1.0.0",
		Init: func(_ context.Context, _ plugin.InitializeParams, h *pserve.Hub) (map[string]any, error) {
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
			       {"id": "panel", "title": "网关面板", "placement": "launcher_section", "hosts": ["127.0.0.1"]}],
			"events": [{"id": "agents", "subscribe": ["agent.finished"]}]},
		"permissions": {"launcher_keys": true}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A plugin's declarative UI: listed where it goes, drawn by the plugin and held to what the apps draw, its actions
// answered; a launcher section reaches its gateway with the key the plugin never sees; events reach it.
func TestPluginUIAndEvents(t *testing.T) {
	h, _ := pluginTestHub(t)
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
		hp := h.plugins.get("example-ui")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	h.notifyDone("done", "mac", "Mac", "s1", "✅ 完成了", "", pushWants{Kind: "done"})
	_, detail := call(t, mux, "GET", "/api/v1/plugins/example-ui/-/", master, "")
	if ui, _ := detail["ui"].([]any); len(ui) != 2 {
		t.Fatalf("its pages: %v", detail["ui"])
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
	if list, _ := places["ui"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != "panel" {
		t.Fatalf("the launcher's sections: %v", places)
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

func TestCheckView(t *testing.T) {
	ratio := 0.4
	ok := plugin.View{Sections: []plugin.Section{{Items: []plugin.UIElem{
		{Type: "text", Text: "x"}, {Type: "progress", Label: "l", Ratio: &ratio, Tone: "caution"},
		{Type: "row", Title: "r", Badge: &plugin.Badge{Text: "b", Tone: "danger"}, Action: &plugin.UIAction{ID: "a"}},
		{Type: "link", Title: "l", URL: "https://example.com"}, {Type: "field", Name: "f", Label: "F", Kind: "choice", Choices: []plugin.UIChoice{{Value: "1"}}},
		{Type: "divider"}}}}}
	if err := plugin.CheckView(&ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string]plugin.UIElem{
		"unknown type":   {Type: "html"},
		"tone":           {Type: "metric", Label: "a", Value: "b", Tone: "#ff0000"},
		"http link":      {Type: "link", Title: "x", URL: "http://example.com"},
		"secret value":   {Type: "field", Name: "s", Label: "S", Kind: "secret", Value: "leak"},
		"button":         {Type: "button", Title: "x"},
		"ratio":          {Type: "progress", Label: "x", Ratio: func() *float64 { v := 1.5; return &v }()},
		"long text":      {Type: "text", Text: strings.Repeat("长", plugin.StringMost+1)},
		"empty choices":  {Type: "field", Name: "c", Label: "C", Kind: "choice"},
		"action without": {Type: "row", Title: "x", Action: &plugin.UIAction{}},
	}
	for name, e := range bad {
		if err := plugin.CheckView(&plugin.View{Sections: []plugin.Section{{Items: []plugin.UIElem{e}}}}); err == nil {
			t.Errorf("%s: drawn", name)
		}
	}
}
