package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
)

// serveSystemExtensionForTest draws a widget (counting its renders, with what a widget does not draw in it), a page
// the widget opens, and runs a shortcut.
func serveSystemExtensionForTest() {
	var hub *pserve.Hub
	var renders atomic.Int32
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-sys", Version: "1.0.0",
		Init: func(_ context.Context, _ extension.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			hub = h
			return nil, nil
		},
		Namespaces: map[string]pserve.Handler{
			"ui": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				var p extension.UIParams
				_ = json.Unmarshal(params, &p)
				if method == "action" && p.Action == "changed" {
					_ = hub.Notify("ui.invalidate", map[string]any{"contribution": "today"})
					return map[string]any{"toast": "ok"}, nil
				}
				if p.Contribution != "today" {
					return map[string]any{"sections": []any{map[string]any{"items": []any{map[string]any{"type": "text", "text": "用量",
						"action": nil}, map[string]any{"type": "button", "title": "变了", "action": map[string]any{"id": "changed"}}}}}}, nil
				}
				n := renders.Add(1)
				return map[string]any{"sections": []any{map[string]any{"title": "今天", "items": []any{
					map[string]any{"type": "metric", "label": "第几次", "value": fmt.Sprint(n), "detail": p.Size + " " + p.Placement},
					map[string]any{"type": "stat_grid", "stats": []any{map[string]any{"label": "a", "value": "1"}, map[string]any{"label": "b", "value": "2"},
						map[string]any{"label": "c", "value": "3"}}},
					map[string]any{"type": "button", "title": "刷新", "action": map[string]any{"id": "x"}},
					map[string]any{"type": "row", "title": "一行", "action": map[string]any{"id": "open"}},
					map[string]any{"type": "text", "text": "一\n二\n三\n四"}}}}}, nil
			},
			"shortcut": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "run" {
					return nil, pserve.UnknownMethod(method)
				}
				var p struct {
					Contribution string            `json:"contribution"`
					Values       map[string]string `json:"values"`
					UIVersion    int               `json:"ui_version"`
				}
				_ = json.Unmarshal(params, &p)
				return map[string]any{"text": fmt.Sprintf("%s：%s ×%s (%s) v%d", p.Contribution, p.Values["note"], p.Values["n"], p.Values["mode"], p.UIVersion),
					"view": map[string]any{"sections": []any{map[string]any{"items": []any{
						map[string]any{"type": "metric", "label": "记了", "value": p.Values["note"]},
						map[string]any{"type": "field", "name": "x", "label": "x", "kind": "text"}}}}}}, nil
			},
		}})
}

func systemPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "sys"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=system exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-sys", "name": "Example System", "version": "1.0.0",
		"engines": {"extension_protocol": 1}, "runtime": {"command": {%q: "bin/sys"}}, "activation_events": ["on_startup"],
		"contributes": {
			"ui": [{"id": "usage", "title": "用量", "placement": "nav_page"},
			       {"id": "today", "title": "今天的用量", "placement": "widget", "families": ["small", "accessory_inline"], "refresh_minutes": 20, "opens": "usage"}],
			"shortcuts": [{"id": "add", "title": "记一笔", "ui": "usage", "params": [
				{"name": "note", "title": "内容", "kind": "text", "required": true},
				{"name": "n", "title": "数量", "kind": "number"},
				{"name": "mode", "title": "方式", "kind": "choice", "choices": [{"value": "a"}, {"value": "b"}]}]}]}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// Places outside the app: an extension's widgets listed on their own, drawn for a family and held to what it draws,
// kept until due and dropped when the extension says they changed (waking the devices that show them, at most once in
// a while); its shortcuts listed and run with checked values; shown to the user before installing.
func TestExtensionWidgetsAndShortcuts(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	_, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", master, `{"source": "dir:`+systemPackage(t)+`"}`)
	shown := fmt.Sprint(pv["permissions"])
	if !strings.Contains(shown, "主屏幕小组件和表盘（在 App 外显示）：今天的用量") || !strings.Contains(shown, "快捷指令和 Siri 里的动作：记一笔") {
		t.Fatalf("widgets and shortcuts are shown before installing: %v", shown)
	}
	call(t, mux, "POST", "/api/v1/extensions/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	waitLong(t, "the extension up", func() bool {
		hp := h.extensions.get("example-sys")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	_, list := call(t, mux, "GET", "/api/v1/extensions/-/widgets", master, "")
	if got := fmt.Sprint(list["widgets"]); !strings.Contains(got, "id:today") || !strings.Contains(got, "refresh_minutes:20") ||
		!strings.Contains(got, "opens:usage") || !strings.Contains(got, "families:[small accessory_inline]") {
		t.Fatalf("widgets: %v", list)
	}
	if _, all := call(t, mux, "GET", "/api/v1/extensions/-/ui", master, ""); strings.Contains(fmt.Sprint(all["ui"]), "id:today") {
		t.Fatalf("a widget is not among the slots drawn in the app: %v", all)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/ui/today", master, `{}`); code != 400 {
		t.Fatalf("a widget is not drawn as a page: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/widget/today", master, `{"context": {"size": "large"}}`); code != 400 {
		t.Fatalf("a family the widget does not draw: %d %v", code, out)
	}

	before := time.Now()
	code, out := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/widget/today", master, `{"context": {"size": "small", "ui_version": 3}}`)
	view := fmt.Sprint(out["view"])
	if code != 200 || !strings.Contains(view, "value:1") || !strings.Contains(view, "small widget") || strings.Contains(view, "button") ||
		strings.Contains(view, "id:open") || strings.Contains(view, "四") || strings.Contains(view, "label:c") {
		t.Fatalf("a small widget: the extension asked at its family, held to what it draws: %d %v", code, out)
	}
	exp, _ := time.Parse(time.RFC3339, fmt.Sprint(out["expires_at"]))
	if d := exp.Sub(before); d < 19*time.Minute || d > 21*time.Minute {
		t.Fatalf("kept until its refresh is due: %v", out["expires_at"])
	}
	if _, again := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/widget/today", master, `{"context": {"size": "small", "ui_version": 3}}`); !strings.Contains(fmt.Sprint(again["view"]), "value:1") {
		t.Fatalf("drawn once while not due: %v", again)
	}
	_, inline := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/widget/today", master, `{"context": {"size": "accessory_inline", "ui_version": 3}}`)
	if secs, _ := inline["view"].(map[string]any)["sections"].([]any); len(secs) != 1 || len(secs[0].(map[string]any)["items"].([]any)) != 1 {
		t.Fatalf("a complication draws one thing: %v", inline)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/devices/phone1/widgets", master, `{"widgets": [{"extension": "example-sys", "id": "today", "family": "small"}]}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/devices/phone1/widgets", master, `{"widgets": [{"extension": "example-sys", "id": "today", "family": "huge"}]}`); code != 400 {
		t.Fatal("a family that is not one")
	}
	call(t, mux, "POST", "/api/v1/extensions/example-sys/-/ui/usage", master, `{"action": "changed"}`)
	w := &h.extensions.widgets
	woken := func() time.Time {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.woken["phone1/example-sys/today"]
	}
	waitLong(t, "the device woken for the widget", func() bool { return !woken().IsZero() })
	first := woken()
	if _, again := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/widget/today", master, `{"context": {"size": "small", "ui_version": 3}}`); !strings.Contains(fmt.Sprint(again["view"]), "value:3") {
		t.Fatalf("drawn again once it changed: %v", again)
	}
	call(t, mux, "POST", "/api/v1/extensions/example-sys/-/ui/usage", master, `{"action": "changed"}`)
	time.Sleep(2 * invalidateWait)
	if !woken().Equal(first) {
		t.Fatal("a device is woken for a widget once in a while, not each time it changes")
	}

	_, sc := call(t, mux, "GET", "/api/v1/extensions/-/shortcuts", master, "")
	if got := fmt.Sprint(sc["shortcuts"]); !strings.Contains(got, "id:add") || !strings.Contains(got, "name:note") || !strings.Contains(got, "ui:usage") {
		t.Fatalf("shortcuts: %v", sc)
	}
	code, out = call(t, mux, "POST", "/api/v1/extensions/example-sys/-/shortcut/add", master, `{"values": {"note": "咖啡", "n": "2", "mode": "b"}, "context": {"ui_version": 3}}`)
	if code != 200 || out["text"] != "add：咖啡 ×2 (b) v3" || strings.Contains(fmt.Sprint(out["view"]), "field") || !strings.Contains(fmt.Sprint(out["view"]), "value:咖啡") {
		t.Fatalf("a shortcut runs, its view held to a large widget's: %d %v", code, out)
	}
	for _, bad := range []string{`{"values": {"n": "2"}}`, `{"values": {"note": "x", "n": "two"}}`, `{"values": {"note": "x", "mode": "c"}}`, `{"values": {"note": "x", "who": "y"}}`} {
		if code, out := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/shortcut/add", master, bad); code != 400 {
			t.Fatalf("%s: %d %v", bad, code, out)
		}
	}
	if code, _ := call(t, mux, "POST", "/api/v1/extensions/example-sys/-/shortcut/nope", master, `{}`); code != 404 {
		t.Fatal("a shortcut the extension does not have")
	}
}
