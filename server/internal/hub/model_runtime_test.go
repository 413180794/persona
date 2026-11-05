package hub

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// 主对话's runtime (#2455): only the assistant takes one, it reaches the node's params, and it is in the fingerprint.

func TestModelRuntimeIsOfferedAndPatchedOnlyForTheAssistant(t *testing.T) {
	h, mux := modelTestHub(t)
	r := modelRequest(mux, "GET", "", "")
	var list struct {
		Purposes []ModelPurpose `json:"purposes"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Purposes {
		want := p.ID == "assistant"
		if got := slices.Equal(p.Runtimes, []string{"cli", "agentsdk"}) && slices.Contains(p.Fields, "runtime"); got != want {
			t.Fatalf("%s: runtimes %v fields %v", p.ID, p.Runtimes, p.Fields)
		}
	}
	cases := []struct {
		id, body string
		code     int
	}{
		{"agent.new", `{"revision":0,"runtime":"agentsdk"}`, 400},
		{"kb", `{"revision":0,"runtime":"cli"}`, 400},
		{"assistant", `{"revision":0,"runtime":"sdk"}`, 400},
		{"assistant", `{"revision":0,"runtime":"agentsdk"}`, 200},
		{"assistant", `{"revision":1,"provider":"launcher.claude"}`, 200}, // a provider change keeps the runtime
	}
	for _, c := range cases {
		if r := modelRequest(mux, "PATCH", c.id, c.body); r.Code != c.code {
			t.Fatalf("%s %s: %d %s", c.id, c.body, r.Code, r.Body.String())
		}
	}
	b, err := h.resolveModel(context.Background(), "assistant")
	if err != nil || b.Runtime != proto.AssistantRuntimeAgentSDK {
		t.Fatalf("%+v %v", b, err)
	}
	if b, _ := h.resolveModel(context.Background(), "schedule"); b.Runtime != proto.AssistantRuntimeAgentSDK {
		t.Fatalf("scheduled tasks run as 主对话: %+v", b)
	}
	if r := modelRequest(mux, "PATCH", "assistant", `{"revision":2,"runtime":null}`); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if b, _ := h.resolveModel(context.Background(), "assistant"); b.Runtime != "" {
		t.Fatalf("runtime not cleared: %+v", b)
	}
}

func TestModelRuntimeReachesParamsAndFingerprint(t *testing.T) {
	h, _ := modelTestHub(t)
	ctx := context.Background()
	run := func(apply func(map[string]any) error, params map[string]any) map[string]any {
		t.Helper()
		if err := apply(params); err != nil {
			t.Fatal(err)
		}
		return params
	}
	assistant := func(p map[string]any) error { return h.applyModel(ctx, "assistant", p) }
	cli := run(assistant, map[string]any{"runtime": "stale"})
	if _, ok := cli["runtime"]; ok {
		t.Fatalf("no runtime is the CLI, nothing sent: %+v", cli)
	}
	explicitCLI, err := patchModel(ModelBinding{}, modelPatch{Runtime: json.RawMessage(`"cli"`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "assistant", 0, explicitCLI); err != nil {
		t.Fatal(err)
	}
	explicit := run(assistant, map[string]any{})
	if explicit["model_fingerprint"] != cli["model_fingerprint"] {
		t.Fatal("an explicit cli restarted the process")
	}
	if err := h.saveModelRecord(ctx, "assistant", 1, ModelBinding{Runtime: proto.AssistantRuntimeAgentSDK}); err != nil {
		t.Fatal(err)
	}
	sdk := run(assistant, map[string]any{})
	if sdk["runtime"] != proto.AssistantRuntimeAgentSDK || sdk["model_fingerprint"] == cli["model_fingerprint"] {
		t.Fatalf("runtime not applied or not fingerprinted: %+v", sdk)
	}
	// Another launcher (warm, a start for one launcher) and a pick from the chat's menu keep 主对话's runtime.
	for name, p := range map[string]map[string]any{
		"launcher": run(assistant, map[string]any{"launcher": "other"}),
		"picked":   run(func(p map[string]any) error { return h.applyPickedModel(ctx, p) }, map[string]any{"launcher": "other", "model": "m"}),
		"schedule": run(func(p map[string]any) error { return h.applyModel(ctx, "schedule", p) }, map[string]any{}),
	} {
		if p["runtime"] != proto.AssistantRuntimeAgentSDK {
			t.Fatalf("%s lost the runtime: %+v", name, p)
		}
	}
	if agent := run(func(p map[string]any) error { return h.applyModel(ctx, "agent.new", p) }, map[string]any{}); agent["runtime"] != nil {
		t.Fatalf("Agents took 主对话's runtime: %+v", agent)
	}
}

func TestModelRuntimeSurvivesAProviderChange(t *testing.T) {
	b := mergeModel(ModelBinding{Provider: "launcher.a", Model: "m", Runtime: "agentsdk"}, ModelBinding{Provider: "launcher.b"})
	if b != (ModelBinding{Provider: "launcher.b", Runtime: "agentsdk"}) {
		t.Fatalf("%+v", b)
	}
}

func TestModelMessagesAPIProviderRunsAsLauncher(t *testing.T) {
	h, mux := modelTestHub(t)
	ctx := context.Background()
	stored := []storedLauncher{
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", Name: "gw", BaseURL: "https://gw.example.com", Model: "m1"}},
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", API: "messages", Name: "gw2", BaseURL: "https://gw2.example.com", Model: "m2"}},
		{ManagedLauncher: proto.ManagedLauncher{Kind: "api", API: "responses", Name: "resp", BaseURL: "https://r.example.com", Model: "gpt"}},
	}
	if err := h.put(ctx, launchersManagedKey, stored); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gw", "gw2"} {
		p := map[string]any{"launcher": name}
		if err := h.applyModel(ctx, "assistant", p); err != nil || p["launcher"] != name {
			t.Fatalf("%s: %v %+v", name, err, p)
		}
	}
	if err := h.applyModel(ctx, "assistant", map[string]any{"launcher": "resp"}); err == nil {
		t.Fatal("a Responses provider ran as a launcher")
	}
	r := modelRequest(mux, "GET", "assistant", "")
	var detail struct {
		Purpose ModelPurpose `json:"purpose"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &detail)
	ids := detail.Purpose.ProviderIDs
	if !slices.Contains(ids, "launcher.gw") || !slices.Contains(ids, "launcher.gw2") || slices.Contains(ids, "launcher.resp") {
		t.Fatalf("assistant providers: %v", ids)
	}
}
