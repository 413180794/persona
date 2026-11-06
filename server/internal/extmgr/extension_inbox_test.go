package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// asInbox makes a plugin package's connector one with an inbox (and fallback when asked), with triage_rules in its
// settings when rules.
func asInbox(fallback, rules bool) func(m map[string]any) {
	return func(m map[string]any) {
		caps := []any{"status", "tools", "inbox"}
		if fallback {
			caps = append(caps, "fallback")
		}
		m["contributes"] = map[string]any{"connectors": []any{map[string]any{"id": "examplenotes", "name": "Example Notes", "capabilities": caps}}}
		if rules {
			conf, _ := m["configuration"].(map[string]any)
			if conf == nil {
				conf = map[string]any{"type": "object", "properties": map[string]any{}}
				m["configuration"] = conf
			}
			conf["properties"].(map[string]any)[extension.TriageRulesField] = map[string]any{"type": "string", "title": "分拣规则"}
		}
	}
}

// withTriageModel gives the hub a triage model it can call, or none (the model configuration is the hub's: it says
// whether inbox.triage can be called, models.triage_usable).
func withTriageModel(t *testing.T, h *Manager, on bool) {
	t.Helper()
	b1TriageModel(h).Store(on)
}

func previewDir(t *testing.T, h *Manager, dir string) map[string]any {
	t.Helper()
	code, pv := call(t, h.routes(), "POST", "/api/v1/plugins/-/preview", h.cfg.Token, `{"source": "dir:`+dir+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, pv)
	}
	return pv
}

func install(t *testing.T, h *Manager, pv map[string]any) (int, map[string]any) {
	t.Helper()
	return call(t, h.routes(), "POST", "/api/v1/plugins/-/install", h.cfg.Token,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
}

// A plugin whose inbox connector has no fallback of its own is not installed (on at once) nor switched on while the
// triage model cannot be called (409 needs_triage_model, saying what to do), and goes once it can; while it is on, the
// model is not changed to one that cannot be called (422 triage_model_in_use, naming it).
func TestInboxExtensionNeedsTheTriageModel(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	pkg := notesPackage(t, "1.0.0")
	editManifest(t, pkg, asInbox(false, true))
	withTriageModel(t, h, false)

	pv := previewDir(t, h, pkg)
	code, out := install(t, h, pv)
	if code != 409 || out["code"] != "needs_triage_model" || out["error"] != "先在 设置 › 模型 里配好『信息筛选』，再启用这个扩展" ||
		h.extensions.reg.Get("example-notes") != nil {
		t.Fatalf("installed without the model: %d %v", code, out)
	}
	withTriageModel(t, h, true)
	if code, out := install(t, h, pv); code != 200 {
		t.Fatalf("the same preview once the model is there: %d %v", code, out)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("off: %d %v", code, out)
	}
	withTriageModel(t, h, false)
	code, out = call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": true}`)
	if code != 409 || out["code"] != "needs_triage_model" || h.extensions.reg.Get("example-notes").Record.Enabled {
		t.Fatalf("switched on without the model: %d %v", code, out)
	}
	withTriageModel(t, h, true)
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": true}`); code != 200 {
		t.Fatalf("on with the model: %d %v", code, out)
	}

	// It is on: the hub does not change the model to one that cannot be called (its 422 triage_model_in_use names
	// what needs it: plugins.need_triage, extmgr/models_link.go).
	needers := func() string {
		raw, err := pluginsCall(t, h, assistantwire.PluginsNeedTriage, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Names []string `json:"names"`
		}
		_ = json.Unmarshal(raw, &out)
		return fmt.Sprint(out.Names)
	}
	if got := needers(); got != "[Example Notes]" {
		t.Fatalf("what needs the model: %s", got)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("off: %d %v", code, out)
	}
	if got := needers(); got != "[]" {
		t.Fatalf("nothing needs it any more: %s", got)
	}
}

// One that brings its own fallback needs no model; one without triage_rules does not install at all.
func TestInboxExtensionFallbackAndRules(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	withTriageModel(t, h, false)
	withFallback := notesPackage(t, "1.0.0")
	editManifest(t, withFallback, asInbox(true, true))
	if code, out := install(t, h, previewDir(t, h, withFallback)); code != 200 {
		t.Fatalf("with its own fallback: %d %v", code, out)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", h.cfg.Token, `{"enabled": false}`); code != 200 {
		t.Fatal(code, out)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", h.cfg.Token, `{"enabled": true}`); code != 200 {
		t.Fatalf("on without the model, with its own fallback: %d %v", code, out)
	}
	if m := h.connectors.Get("examplenotes"); m != nil && !m.Manifest().Has(connector.CapFallback) {
		t.Fatalf("manifest %+v", m.Manifest().Capabilities)
	}

	noRules := notesPackage(t, "1.0.0")
	editManifest(t, noRules, func(m map[string]any) {
		asInbox(true, false)(m)
		m["id"] = "example-norules"
		m["contributes"].(map[string]any)["connectors"].([]any)[0].(map[string]any)["id"] = "examplenorules"
	})
	code, out := call(t, mux, "POST", "/api/v1/plugins/-/preview", h.cfg.Token, `{"source": "dir:`+noRules+`"}`)
	if code == 200 || !strings.Contains(fmt.Sprint(out["error"]), extension.TriageRulesField) {
		t.Fatalf("previewed without triage_rules: %d %v", code, out)
	}
}

// chatInbox is a hub.json plugin's inbox connector that counts its collections.
type chatInbox struct{ n atomic.Int32 }

func (*chatInbox) ID() string   { return "examplechat" }
func (*chatInbox) Name() string { return "Example Chat" }
func (*chatInbox) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}}
}
func (c *chatInbox) Collect(context.Context) ([]connector.InboxThing, error) {
	c.n.Add(1)
	return nil, nil
}
func (*chatInbox) Reply(context.Context, connector.InboxReply) error { return nil }

// A hub.json plugin cannot be switched off: one whose inbox needs the triage model runs, but its inbox is not collected
// and Settings › Extensions says why, until the model can be called.
func TestHubJSONInboxExtensionWaitsForTheModel(t *testing.T) {
	h, _ := extensionTestHub(t)
	withTriageModel(t, h, false)
	items, _ := json.Marshal([]extension.ConnectorItem{{ID: "examplechat", Name: "Example Chat", Capabilities: []string{"inbox"}}})
	m := &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: "example-chat", Name: "Example Chat", Version: "0.0.0",
		Contributes: map[string]json.RawMessage{"connectors": items}}
	if err := h.extensions.reg.AddHubJSON(m, nil, true); err != nil {
		t.Fatal(err)
	}
	extensionConnectors.Store("examplechat", extension.SourceHubJSON)
	t.Cleanup(func() { extensionConnectors.Delete("examplechat") })
	c := &chatInbox{}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	status := func() (string, string) {
		v := h.extensions.view(context.Background(), h.extensions.reg.Get("example-chat"))
		return v.Status, strings.Join(v.NeedsSetup, "、")
	}
	h.readConnectors(context.Background())
	if st, needs := status(); c.n.Load() != 0 || st != extensionNeedsSetup || needs != needsTriageModelSetup(i18n.Default) {
		t.Fatalf("collected %d time(s), status %s %q", c.n.Load(), st, needs)
	}
	withTriageModel(t, h, true)
	h.readConnectors(context.Background())
	if _, needs := status(); c.n.Load() != 1 || strings.Contains(needs, needsTriageModelSetup(i18n.Default)) {
		t.Fatalf("with the model: collected %d time(s), needs %q", c.n.Load(), needs)
	}
}

// The market's auto-upgrade goes through the same checks: a version whose inbox has no triage_rules, or one that would
// need the triage model the hub has not, is not installed, and the plugin says why.
func TestExtensionAutoUpgradeInboxRules(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(m map[string]any)
		detail string
	}{
		{"no triage_rules", func(m map[string]any) { addInbox(m, false) }, extension.TriageRulesField},
		{"needs the model", func(m map[string]any) { addInbox(m, true) }, "配好『信息筛选』"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, mux, phone, mk, dir, mid := upgradeHub(t)
			withTriageModel(t, h, false)
			ctx := context.Background()
			setUIVersion(t, dir, "1.2.0")
			editManifest(t, dir, c.change)
			mk.publish(dir)
			_, _ = h.marketRead(ctx, mid)
			h.extensionUpgrades().upgrade(ctx)
			if v := installedVersion(h); v != "1.0.0" {
				t.Fatalf("upgraded to %s", v)
			}
			_, d := call(t, mux, "GET", "/api/v1/plugins/example-ui/-/", phone, "")
			n, _ := d["upgrade_note"].(map[string]any)
			if n["kind"] != upgradeFailed || !strings.Contains(fmt.Sprint(n["detail"]), c.detail) {
				t.Fatalf("note %v", d["upgrade_note"])
			}
		})
	}
}

// addInbox gives the example UI plugin an inbox connector without a fallback, with triage_rules when rules.
func addInbox(m map[string]any, rules bool) {
	contributes := m["contributes"].(map[string]any)
	contributes["connectors"] = []any{map[string]any{"id": "exampleui", "name": "Example UI", "capabilities": []any{"status", "inbox"}}}
	if rules {
		conf, _ := m["configuration"].(map[string]any)
		if conf == nil {
			conf = map[string]any{"type": "object", "properties": map[string]any{}}
			m["configuration"] = conf
		}
		conf["properties"].(map[string]any)[extension.TriageRulesField] = map[string]any{"type": "string"}
	}
}
