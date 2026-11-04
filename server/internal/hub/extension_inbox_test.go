package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
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

// withTriageModel gives the hub a triage model it can call (hub.json's inbox.triage_ai); off, none.
func withTriageModel(t *testing.T, h *Hub, on bool) {
	t.Helper()
	if !on {
		h.cfg.Inbox.TriageAI = nil
		return
	}
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	h.cfg.Inbox.TriageAI = &ai.Config{BaseURL: "http://127.0.0.1:9", APIKeyFile: key, Model: "m"}
}

func previewDir(t *testing.T, h *Hub, dir string) map[string]any {
	t.Helper()
	code, pv := call(t, h.routes(), "POST", "/api/v1/plugins/-/preview", h.cfg.Token, `{"source": "dir:`+dir+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, pv)
	}
	return pv
}

func install(t *testing.T, h *Hub, pv map[string]any) (int, map[string]any) {
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

	// It is on: the model is not changed to one that cannot be called (hub.json's "ai" is empty here).
	code, out = call(t, mux, "PATCH", "/api/v1/model-config/purposes/inbox.triage", master, `{"revision": 0, "provider": "legacy.ai"}`)
	if code != 422 || out["code"] != "triage_model_in_use" || fmt.Sprint(out["plugins"]) != "[Example Notes]" ||
		!strings.Contains(out["error"].(string), "Example Notes") {
		t.Fatalf("cleared the model under it: %d %v", code, out)
	}
	// Clearing the override is fine while what it falls back to (hub.json's inbox.triage_ai) can be called.
	if code, out := call(t, mux, "PATCH", "/api/v1/model-config/purposes/inbox.triage", master, `{"revision": 0, "provider": null}`); code == 409 {
		t.Fatalf("cleared to a callable fallback: %d %v", code, out)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("off: %d %v", code, out)
	}
	if code, out := call(t, mux, "PATCH", "/api/v1/model-config/purposes/inbox.triage", master, `{"revision": 1, "provider": "legacy.ai"}`); code == 409 {
		t.Fatalf("nothing needs it any more: %d %v", code, out)
	}
}

// One that brings its own fallback needs no model; one without triage_rules does not install at all.
func TestInboxExtensionFallbackAndRules(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
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
// and 设置 › 扩展 says why, until the model can be called.
func TestHubJSONInboxExtensionWaitsForTheModel(t *testing.T) {
	h, _ := extensionTestHub(t)
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
	h.collectConnectors(context.Background())
	if st, needs := status(); c.n.Load() != 0 || st != extensionNeedsSetup || needs != needsTriageModelSetup {
		t.Fatalf("collected %d time(s), status %s %q", c.n.Load(), st, needs)
	}
	withTriageModel(t, h, true)
	h.collectConnectors(context.Background())
	if _, needs := status(); c.n.Load() != 1 || strings.Contains(needs, needsTriageModelSetup) {
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

// A plugin connector's thing with its own fallback (extra.rule_tier, capability fallback) and no tier is still sorted
// by the model; when the model cannot, it comes in with that fallback (按规则) instead of being held back.
func TestExtensionThingFallbackTier(t *testing.T) {
	_, client := newTriageModel(t, "fail", `{"tier":"action","summary":"示例","reason":"示例"}`)
	th := genericThing("example-org/example-repo#8", "a", "示例：一条评论")
	th.Extra = &connector.ThingExtra{RuleTier: TierFYI}
	h, st := admitHub(t, client, &inboxConnector{things: []connector.InboxThing{th}})
	collectAndAdmit(h)
	it := itemOf(h, "example", th.Thread)
	if it == nil || it.Tier != TierFYI || it.Reason != "按规则" || it.TriagedAt != it.Last || !st.saved(it.ID) {
		t.Fatalf("item %+v", it)
	}
	if _, status := inboxTriageOf(t, h); status.Waiting != 0 {
		t.Fatalf("held back: %+v", status)
	}
}
