package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

// Sign-ins in ego: only the user's own apps, passed straight to the node, logged without a value.
func TestBrowserCookies(t *testing.T) {
	h, _ := pluginTestHub(t)
	ctx := context.Background()
	mac, seen := egoMac(t, h, "mac")
	var logged bytes.Buffer
	h.logger = log.New(&logged, "", 0)
	handler := h.withMachine(h.browserCookies)
	do := func(method, query, body string, own bool) (int, map[string]any) {
		req := httptest.NewRequest(method, "/api/v1/machines/mac/browser/cookies"+query, strings.NewReader(body))
		req.SetPathValue("mid", "mac")
		if own {
			req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{ID: "dev_phone", Name: "iPhone", Role: roleFull}))
		} else { // the pairing token in hub.json: the CLI, the assistant's processes
			req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{Role: roleFull}))
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	if code, _ := do("GET", "", "", true); code != 404 {
		t.Fatalf("plugin off: %d", code)
	}
	if err := h.plugins.reg.SetEnabled(ctx, egoPluginID, true, "test"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"GET", "POST", "DELETE"} {
		if code, _ := do(m, "?all=1", `{"cookies":[{"name":"a","value":"v","domain":"example.com"}]}`, false); code != 403 {
			t.Fatalf("%s by the pairing token: %d", m, code)
		}
	}
	if code, out := do("GET", "", "", true); code != 200 || len(out["sites"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, out)
	}
	body := `{"cookies":[{"name":"sid","value":"s3cr3t-value","domain":".example.com","secure":true},{"name":"b","value":"v2","domain":"shop.example.org"}]}`
	if code, out := do("POST", "", body, true); code != 200 || out["imported"] != 2.0 {
		t.Fatalf("import: %d %v", code, out)
	}
	if code, out := do("DELETE", "?all=1", "", true); code != 200 || out["removed"] != 2.0 || out["sites"] == nil {
		t.Fatalf("clear: %d %v", code, out)
	}
	if code, _ := do("DELETE", "", "", true); code != 400 {
		t.Fatalf("clear nothing: %d", code)
	}
	if code, _ := do("POST", "", `{"cookies":[]}`, true); code != 400 {
		t.Fatalf("import nothing: %d", code)
	}
	var imported []any
	for _, c := range seen() {
		if c.Method == "browser.cookies.import" {
			imported = c.Params["cookies"].([]any)
		}
	}
	if len(imported) != 2 || imported[0].(map[string]any)["value"] != "s3cr3t-value" {
		t.Fatalf("the node got %v", imported)
	}
	if strings.Contains(logged.String(), "s3cr3t") || !strings.Contains(logged.String(), "import 2 for example.com,shop.example.org by iPhone") {
		t.Fatalf("log: %s", logged.String())
	}
	mac.mu.Lock()
	mac.online = false
	mac.mu.Unlock()
	if code, _ := do("GET", "", "", true); code != 409 {
		t.Fatalf("offline: %d", code)
	}
}
