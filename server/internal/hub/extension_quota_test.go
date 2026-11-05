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
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
	"github.com/nwverse/persona/server/internal/quota"
	"github.com/nwverse/persona/server/internal/rpc"
)

// serveQuotaPluginForTest is a quota plugin: it asks the launcher's gateway for /me/usage with the key in its
// Authorization (a gateway that answers 404 is not its), and reports what it saw — never the key itself.
func serveQuotaExtensionForTest() {
	var hub *pserve.Hub
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-quota", Version: "1.0.0",
		Init: func(_ context.Context, _ extension.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			hub = h
			return nil, nil
		},
		Namespaces: map[string]pserve.Handler{"quota": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			if method != "query" {
				return nil, pserve.UnknownMethod(method)
			}
			var p struct {
				QueryID string `json:"query_id"`
				BaseURL string `json:"base_url"`
			}
			_ = json.Unmarshal(params, &p)
			// Another plugin's query, and somewhere else than the gateway: both refused.
			var probe quota.Response
			stolen := hub.Call(ctx, "quota.http", map[string]any{"query_id": "not-mine", "url": "/me/usage"}, &probe)
			away := hub.Call(ctx, "quota.http", map[string]any{"query_id": p.QueryID, "url": "https://elsewhere.example.com/x",
				"headers": map[string]string{"Authorization": "Bearer " + quota.KeyPlaceholder}}, &probe)
			var resp quota.Response
			if err := hub.Call(ctx, "quota.http", map[string]any{"query_id": p.QueryID, "method": "GET", "url": "/me/usage",
				"headers": map[string]string{"Authorization": "Bearer " + quota.KeyPlaceholder}}, &resp); err != nil {
				return nil, err
			}
			if resp.Status == http.StatusNotFound {
				return nil, &rpc.Error{Code: extension.CodeUnsupported, Message: "not this gateway"}
			}
			note := fmt.Sprintf("stolen=%v away=%v body=%s", stolen != nil, away != nil, resp.Body)
			return map[string]any{"report": quota.Report{KeyName: "example key", Note: note,
				Usage: []quota.Usage{{Label: "今日", Requests: 3}}}}, nil
		}}})
}

func quotaPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "quota"), []byte("#!/bin/sh\nJARVIS_TEST_PLUGIN=quota exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-quota", "name": "Example 额度", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/quota"}},
		"contributes": {"quota": [{"id": "example-gw", "name": "Example 网关"}]},
		"permissions": {"launcher_keys": true}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A launcher's quota is read by a quota plugin that never sees its key: whoever holds the key (the assistant for one
// added in Persona) sends its requests with the key put in, to the launcher's own gateway only, and takes the key out
// of the answer.
func TestQuotaThroughAnExtension(t *testing.T) {
	h, _ := extensionTestHub(t)
	connectAssistant(t, h) // the launchers added in Persona are the assistant's, and so are their keys
	mux := h.routes()
	master := h.cfg.Token
	const key = "sk-example-launcher-key"
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprintf(w, `{"key":%q,"today":3}`, key)
	}))
	defer gw.Close()
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	for name, base := range map[string]string{"gwlauncher": gw.URL + "/v1", "otherlauncher": other.URL} {
		if code, out := call(t, mux, "POST", "/api/v1/launchers", master,
			fmt.Sprintf(`{"name": %q, "base_url": %q, "model": "example-model", "api_key": %q}`, name, base, key)); code != 200 {
			t.Fatalf("%d %v", code, out)
		}
	}

	_, out := call(t, mux, "GET", "/api/v1/launchers/gwlauncher/quota", master, "")
	if out["unsupported"] != true || !strings.Contains(fmt.Sprint(out["error"]), "额度扩展") {
		t.Fatalf("no quota plugin yet: %v", out)
	}

	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+quotaPackage(t)+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}

	_, out = call(t, mux, "GET", "/api/v1/launchers/gwlauncher/quota?refresh=1", master, "")
	q, _ := out["quota"].(map[string]any)
	if q == nil {
		t.Fatalf("%v", out)
	}
	note := fmt.Sprint(q["note"])
	if q["gateway"] != "example-gw" || q["gateway_name"] != "Example 网关" || q["key_name"] != "example key" {
		t.Fatalf("%v", q)
	}
	if strings.Contains(note, key) || !strings.Contains(note, quota.KeyPlaceholder) {
		t.Fatalf("the plugin saw the key: %s", note)
	}
	if !strings.Contains(note, "stolen=true") || !strings.Contains(note, "away=true") {
		t.Fatalf("another query's or another host's request went out: %s", note)
	}

	_, out = call(t, mux, "GET", "/api/v1/launchers/otherlauncher/quota?refresh=1", master, "")
	if out["unsupported"] != true {
		t.Fatalf("a gateway the plugin does not read: %v", out)
	}
}

// A plugin's model provider is offered when a launcher is added, and fills in what the user leaves out.
func TestProviderPresets(t *testing.T) {
	h, store := extensionTestHub(t)
	connectAssistant(t, h) // adding a provider is the assistant's; the presets stay the hub's (the plugins')
	mux := h.routes()
	master := h.cfg.Token
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(`{"manifest_version": 1, "id": "example-providers", "name": "Example 供应商",
		"version": "1.0.0", "contributes": {"providers": [{"id": "example-ai", "name": "Example AI", "kind": "api", "api": "responses",
		"base_url": "https://api.example.com/v1", "models": [{"id": "example-large", "name": "Large"}, {"id": "example-small"}],
		"key_hint": "在控制台 › API Keys 里创建"}]}}`), 0o644)
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+dir+`"}`)
	if lines := pv["permissions"].([]any); len(lines) != 0 {
		t.Fatalf("a declarative plugin runs nothing and asks nothing: %v", lines)
	}
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 || out["status"] != "running" {
		t.Fatalf("%d %v", code, out)
	}
	_, out := call(t, mux, "GET", "/api/v1/launchers/presets", master, "")
	presets := out["presets"].([]any)
	if len(presets) != 1 {
		t.Fatal(presets)
	}
	pr := presets[0].(map[string]any)
	if pr["id"] != "example-providers/example-ai" || pr["kind"] != "api" || len(pr["models"].([]any)) != 2 {
		t.Fatalf("%v", pr)
	}
	if code, out := call(t, mux, "POST", "/api/v1/launchers", master, `{"name": "exampleai", "preset": "example-providers/example-ai", "api_key": "sk-x"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	} else if l := out["launcher"].(map[string]any); l["base_url"] != "https://api.example.com/v1" || l["kind"] != "api" || l["api"] != "responses" || l["model"] != "example-large" {
		t.Fatalf("filled in: %v", l)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/launchers", master, `{"name": "nope", "preset": "example-providers/gone", "api_key": "k"}`); code != 400 {
		t.Fatal("an unknown preset:", code)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/launchers", master, `{"name": "presets", "base_url": "https://x.example.com", "model": "m", "api_key": "k"}`); code != 400 {
		t.Fatal("presets is not a launcher name:", code)
	}
	var stored []struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	_, _ = store.Setting(context.Background(), "launchers.managed", &stored) // the assistant's
	if len(stored) != 1 || stored[0].Preset != "example-providers/example-ai" {
		t.Fatalf("%+v", stored)
	}
}
