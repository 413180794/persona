package extmgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
)

// installForTest installs the package at dir with the master token, answering its detail.
func installForTest(t *testing.T, h *Manager, dir string) map[string]any {
	t.Helper()
	mux := h.routes()
	code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", h.cfg.Token, `{"source": "dir:`+dir+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, pv)
	}
	code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", h.cfg.Token,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	if code != 200 {
		t.Fatalf("install: %d %v", code, out)
	}
	return out
}

// A declarative extension's providers switched one by one: the presets follow, the switch holds, the old path works.
func TestContributionSwitchRoute(t *testing.T) {
	h, store := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := t.TempDir()
	manifest := `{"manifest_version": 1, "id": "example-models", "name": "Example Models", "version": "1.0.0",
		"contributes": {"providers": [
			{"id": "fast", "name": "Fast", "base_url": "https://fast.example.com"},
			{"id": "slow", "name": "Slow", "base_url": "https://slow.example.com"}]}}`
	if err := os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	installForTest(t, h, dir)

	presets := func() []string { // what the manager's GET /api/v1/launchers/presets gets (plugins.presets)
		var ids []string
		for _, p := range h.extensions.launcherPresets(context.Background()) {
			ids = append(ids, p.ID)
		}
		return ids
	}
	if got := presets(); len(got) != 2 {
		t.Fatal(got)
	}
	code, out := call(t, mux, "PUT", "/api/v1/extensions/example-models/-/contributions/providers/slow", master, `{"enabled": false}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	cs := out["contributions"].([]any)
	if len(cs) != 2 || cs[1].(map[string]any)["id"] != "slow" || cs[1].(map[string]any)["enabled"] != false ||
		cs[0].(map[string]any)["enabled"] != true || cs[1].(map[string]any)["title"] != "Slow" || cs[1].(map[string]any)["can_disable"] != true {
		t.Fatalf("%v", cs)
	}
	if got := presets(); len(got) != 1 || got[0] != "example-models/fast" {
		t.Fatal("the switched-off provider is gone from the presets:", got)
	}
	if c := out["contributes"].(map[string]any)["providers"].([]any); len(c) != 2 {
		t.Fatal("contributes still lists every contribution:", c)
	}
	if got := store.rows["example-models"].DisabledContributions; len(got) != 1 || got[0] != "providers/slow" {
		t.Fatal(got)
	}
	// The old path reaches the same route.
	if code, out = call(t, mux, "PUT", "/api/v1/plugins/example-models/-/contributions/providers/slow", master, `{"enabled": true}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if got := presets(); len(got) != 2 {
		t.Fatal(got)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/example-models/-/contributions/providers/nope", master, `{"enabled": false}`); code != 404 {
		t.Fatal("no such contribution:", code)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/example-models/-/contributions/providers/slow", master, `{}`); code != 400 {
		t.Fatal("enabled is required:", code)
	}
}

// A connector's contribution switch is the connector's own switch: both ways.
func TestContributionSwitchOfAConnector(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	installForTest(t, h, notesPackage(t, "1.0.0"))
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/example-notes/-/secrets/api_key", master, `{"value": "k3y"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	waitLong(t, "the connector", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})
	code, out := call(t, mux, "PUT", "/api/v1/extensions/example-notes/-/contributions/connectors/examplenotes", master, `{"enabled": false}`)
	if code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if h.connectors.Enabled("examplenotes") {
		t.Fatal("the connector's switch is off")
	}
	c := out["contributions"].([]any)[0].(map[string]any)
	if c["via"] != "connector" || c["enabled"] != false || c["can_disable"] != false {
		t.Fatalf("%v", c)
	}
	if err := h.setConnectorEnabled(context.Background(), "examplenotes", true); err != nil {
		t.Fatal(err)
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/example-notes/-/", master, "")
	if c := out["contributions"].([]any)[0].(map[string]any); c["enabled"] != true {
		t.Fatal("switched on in 设置 › 连接器, on here:", c)
	}
}
