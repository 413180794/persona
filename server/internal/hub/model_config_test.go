package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/proto"
)

type modelTestStore struct {
	Store
	mu     sync.Mutex
	values map[string]json.RawMessage
	fail   bool
}

func (s *modelTestStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return false, errors.New("database unavailable")
	}
	raw, ok := s.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}
func (s *modelTestStore) PutSetting(_ context.Context, key string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("database unavailable")
	}
	raw, err := json.Marshal(v)
	if err == nil {
		s.values[key] = raw
	}
	return err
}

func modelTestHub(t *testing.T) (*Hub, *http.ServeMux) {
	h := newTestHub(t)
	h.store = &modelTestStore{values: map[string]json.RawMessage{}}
	mux := http.NewServeMux()
	h.modelConfigRoutes(mux)
	return h, mux
}
func modelRequest(mux http.Handler, method, id, body string) *httptest.ResponseRecorder {
	path := "/api/v1/model-config"
	if id != "" {
		path += "/purposes/" + id
	}
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	return r
}

func TestModelConfigurationReadFailureIsNotAnOldHub(t *testing.T) {
	h, mux := modelTestHub(t)
	h.store.(*modelTestStore).fail = true
	for _, method := range []string{"GET", "PATCH"} {
		body := ""
		if method == "PATCH" {
			body = `{"revision":0,"model":"selected"}`
		}
		if r := modelRequest(mux, method, "agent.new", body); r.Code != http.StatusInternalServerError {
			t.Fatalf("%s database failure returned %d instead of 500: %s", method, r.Code, r.Body.String())
		}
	}
	h.store.(*modelTestStore).fail = false
	if r := modelRequest(mux, "GET", "missing", ""); r.Code != http.StatusNotFound {
		t.Fatalf("unknown purpose returned %d", r.Code)
	}
}

func TestModelConfigurationExtensionFailureIsVisible(t *testing.T) {
	h, mux := modelTestHub(t)
	h.extensions = newExtensions(t.TempDir(), []ExtensionConfig{{ID: "voice", Name: "Voice", Kind: "voice", Socket: "/nonexistent/model-test.sock"}}, t.Logf)
	r := modelRequest(mux, "GET", "plugin.voice", "")
	if r.Code != http.StatusOK {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	var result struct {
		Purpose ModelPurpose `json:"purpose"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Purpose.Status != "读不到扩展模型列表" {
		t.Fatalf("plugin failure was hidden: %+v", result.Purpose)
	}
}

func TestModelConfigurationPatchIsSparseAndVersioned(t *testing.T) {
	h, mux := modelTestHub(t)
	h.cfg.AI = ai.Config{API: "responses", BaseURL: "https://api.example.com", APIKeyFile: "/tmp/test-key", Model: "original", ReasoningEffort: "low"}
	r := modelRequest(mux, "PATCH", "inbox.triage", `{"revision":0,"model":"selected"}`)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	r = modelRequest(mux, "PATCH", "inbox.triage", `{"revision":1,"effort":"high"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	b, err := h.resolveModel(context.Background(), "inbox.triage")
	if err != nil || b.Model != "selected" || b.Effort != "high" {
		t.Fatalf("%+v %v", b, err)
	}
	r = modelRequest(mux, "PATCH", "inbox.triage", `{"revision":0,"model":"stale"}`)
	if r.Code != 409 {
		t.Fatalf("stale write accepted: %d", r.Code)
	}
	r = modelRequest(mux, "PATCH", "inbox.triage", `{"revision":2,"model":null}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	b, _ = h.resolveModel(context.Background(), "inbox.triage")
	if b.Model != "original" || b.Effort != "high" {
		t.Fatalf("restore inheritance: %+v", b)
	}
	r = modelRequest(mux, "PATCH", "kb", `{"revision":0,"model":"knowledge"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	b, _ = h.resolveModel(context.Background(), "inbox.triage")
	if b.Model != "original" {
		t.Fatal("another purpose overwrote triage")
	}
	for _, body := range []string{`{"revision":3,"model":""}`, `{"revision":3,"temperature":0.1}`, `{"model":"x"}`} {
		if r := modelRequest(mux, "PATCH", "inbox.triage", body); r.Code != 400 {
			t.Fatalf("invalid patch accepted: %s (%d)", body, r.Code)
		}
	}
}

func TestModelConfigurationPreservesLegacyBaselines(t *testing.T) {
	h, _ := modelTestHub(t)
	ctx := context.Background()
	h.cfg.AI = ai.Config{Model: "base", ReasoningEffort: "low"}
	h.cfg.Inbox.TriageAI = &ai.Config{Model: "triage"}
	h.cfg.Inbox.JudgeAI = &ai.Config{Model: "judge"}
	h.cfg.KB.AI = &ai.Config{Model: "knowledge"}
	h.cfg.Assistant.Launcher = "conversation"
	h.cfg.Inbox.Agent.Launcher = "delegate"
	for id, want := range map[string]string{"inbox.triage": "triage", "inbox.judge": "triage", "translate": "knowledge", "kb.compress": "knowledge", "assistant.title": "knowledge"} {
		b, err := h.resolveModel(ctx, id)
		if err != nil || b.Model != want {
			t.Errorf("%s: %+v %v", id, b, err)
		}
	}
	for id, want := range map[string]string{"assistant": "conversation", "lark.agent": "claude", "inbox.agent": "claude"} {
		b, err := h.resolveModel(ctx, id)
		if err != nil || b.Provider != launcherProvider(want) {
			t.Errorf("%s: %+v %v", id, b, err)
		}
	}
	if err := h.saveModelRecord(ctx, "lark.agent", 0, ModelBinding{Provider: "launcher.new"}); err != nil {
		t.Fatal(err)
	}
	b, _ := h.resolveModel(ctx, "lark.agent")
	if b.Provider != "launcher.claude" {
		t.Fatal("deprecated use override still applied")
	}
	if err := h.saveModelRecord(ctx, "agent.new", 0, ModelBinding{Provider: "launcher.new"}); err != nil {
		t.Fatal(err)
	}
	b, _ = h.resolveModel(ctx, "lark.agent")
	if b.Provider != "launcher.new" {
		t.Fatal("category selection did not reach the slash agent")
	}
}

func TestAllAPIModelPurposesReachTheSelectedModel(t *testing.T) {
	h, mux := modelTestHub(t)
	ctx := context.Background()
	var model string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		model = in.Model
		if r.Header.Get("x-api-key") != "private-test-key" {
			t.Error("wrong API credential")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "{}"}}})
	}))
	defer srv.Close()
	key, err := h.sealKey("private-test-key")
	if err != nil {
		t.Fatal(err)
	}
	provider := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "direct", Kind: "api", API: "messages", BaseURL: srv.URL, Model: "provider-default"}, KeySealed: key}
	if err := h.put(ctx, launchersManagedKey, []storedLauncher{provider}); err != nil {
		t.Fatal(err)
	}
	revisions := map[string]int{}
	for _, p := range modelPurposes {
		if p.Kind != "api" {
			continue
		}
		category := modelCategoryID(p.ID)
		r := modelRequest(mux, "PATCH", category, fmt.Sprintf(`{"revision":%d,"provider":"launcher.direct","model":"%s"}`, revisions[category], p.ID))
		revisions[category]++
		if r.Code != 200 {
			t.Fatalf("%s: %d %s", p.ID, r.Code, r.Body.String())
		}
		if _, err := h.purposeModel(p.ID).Complete(ctx, "", "", 10); err != nil {
			t.Fatal(err)
		}
		if model != p.ID {
			t.Fatalf("%s sent %s", p.ID, model)
		}
	}
	listing := modelRequest(mux, "GET", "", "").Body.String()
	if strings.Contains(listing, "private-test-key") || strings.Contains(listing, key) {
		t.Fatal("credential returned to client")
	}
	if managed := h.managedForNodes(ctx); len(managed) != 0 {
		t.Fatal("API-only provider sent to nodes")
	}
}

// A launcher for a gateway that takes only Bearer keeps that when added in the app, goes to the nodes with it, and the
// hub's own Messages calls through it send Authorization: Bearer.
func TestBearerLauncherSavedSentAndCalled(t *testing.T) {
	h, mux := modelTestHub(t)
	h.launcherRoutes(mux)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer private-test-key" || r.Header.Get("x-api-key") != "" {
			t.Errorf("credential sent as authorization %q, x-api-key %q", r.Header.Get("authorization"), r.Header.Get("x-api-key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}})
	}))
	defer srv.Close()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/launchers",
		strings.NewReader(`{"name":"gw","base_url":"`+srv.URL+`","model":"m","api_key":"private-test-key","auth":"bearer"}`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"auth":"bearer"`) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if managed := h.managedForNodes(ctx); len(managed) != 1 || !managed[0].BearerAuth() || managed[0].APIKey != "private-test-key" {
		t.Fatalf("sent to nodes: %+v", managed)
	}
	config, err := h.apiConfig(ctx, ModelBinding{Provider: "launcher.gw"})
	if err != nil {
		t.Fatal(err)
	}
	if text, err := ai.New(config).Complete(ctx, "", "hi", 10); err != nil || text != "ok" {
		t.Fatalf("complete: %q %v", text, err)
	}
	// A change from an app that predates auth sends none: the launcher stays bearer. One that says api_key changes it.
	for _, step := range []struct {
		body   string
		bearer bool
	}{
		{`{"base_url":"` + srv.URL + `","model":"m2"}`, true},
		{`{"base_url":"` + srv.URL + `","model":"m2","auth":"api_key"}`, false},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/v1/launchers/gw", strings.NewReader(step.body)))
		if rec.Code != 200 {
			t.Fatalf("update %s: %d %s", step.body, rec.Code, rec.Body.String())
		}
		if managed := h.managedForNodes(ctx); len(managed) != 1 || managed[0].BearerAuth() != step.bearer || managed[0].APIKey != "private-test-key" {
			t.Fatalf("after %s: %+v", step.body, managed)
		}
	}
}

func TestModelProviderSwitchDoesNotInheritAnotherProvidersModel(t *testing.T) {
	b := mergeModel(ModelBinding{Provider: "legacy.ai", Model: "old-model", Effort: "high"}, ModelBinding{Provider: "launcher.other"})
	if b.Model != "" || b.Effort != "" {
		t.Fatalf("mixed provider fields: %+v", b)
	}
}

func TestExplicitLauncherResetsPurposeSpecificModel(t *testing.T) {
	h, _ := modelTestHub(t)
	ctx := context.Background()
	if err := h.saveModelRecord(ctx, "agent.new", 0, ModelBinding{Provider: "launcher.original", Model: "specific", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"launcher": "different"}
	if err := h.applyModel(ctx, "agent.new", params); err != nil {
		t.Fatal(err)
	}
	if params["model"] != nil || params["effort"] != nil {
		t.Fatalf("old provider options inherited: %+v", params)
	}
	params = map[string]any{"launcher": "different", "model": "request-model"}
	if err := h.applyModel(ctx, "agent.new", params); err != nil {
		t.Fatal(err)
	}
	if params["model"] != "request-model" {
		t.Fatal("explicit request model lost")
	}
}

func TestModelDatabaseFailureDoesNotFallback(t *testing.T) {
	h, _ := modelTestHub(t)
	h.cfg.AI = ai.Config{BaseURL: "https://api.example.com", Model: "old", APIKeyFile: "/tmp/key"}
	h.store.(*modelTestStore).fail = true
	if _, err := h.purposeModel("inbox.triage").Complete(context.Background(), "", "", 1); err == nil {
		t.Fatal("database failure silently treated as unconfigured")
	}
}

func TestTaskModelConfigurationSurvivesOrdinaryTaskSave(t *testing.T) {
	h, mux := modelTestHub(t)
	ctx := context.Background()
	h.cfg.Assistant.Launcher = "claude"
	seedTasks(t, h, assistant.Task{ID: "one", Title: "任务", Prompt: "x", Repeat: "interval", EveryMinutes: 30, Enabled: true})
	connectAssistant(t, h)
	r := modelRequest(mux, "PATCH", "assistant", `{"revision":0,"provider":"launcher.claude","model":"exact","effort":"high"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	h.assistantRoutesRelayed(mux)
	request := httptest.NewRequest("PUT", "/api/v1/assistant/tasks/one", strings.NewReader(`{"title":"新任务","prompt":"y","repeat":"interval","every_minutes":45,"enabled":true}`))
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, request)
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	b, err := h.resolveModel(ctx, "task.one")
	if err != nil || b.Model != "exact" || b.Effort != "high" {
		t.Fatalf("ordinary task edit overwrote model: %+v %v", b, err)
	}
}

// #388: with no hub.json "ai", API uses run on a keyed connection the hub already has instead of failing, and say so.
func TestAPIModelPurposesFallBackToAKeyedConnection(t *testing.T) {
	h, mux := modelTestHub(t)
	ctx := context.Background()
	h.cfg.AI = ai.Config{}
	var model string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		model = in.Model
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}})
	}))
	defer srv.Close()
	key, err := h.sealKey("private-test-key")
	if err != nil {
		t.Fatal(err)
	}
	purpose := func(id string) ModelPurpose {
		r := modelRequest(mux, "GET", id, "")
		if r.Code != 200 {
			t.Fatalf("%s: %d %s", id, r.Code, r.Body.String())
		}
		var out struct {
			Purpose ModelPurpose `json:"purpose"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Purpose
	}

	// Nothing to call: the status and the error say where to fix it, not only hub.json.
	if p := purpose("inbox.draft"); p.Status != modelNoAPIStatus {
		t.Fatalf("unconfigured status: %+v", p)
	}
	if _, err := h.purposeModel("inbox.draft").Complete(ctx, "", "", 10); err == nil || !strings.Contains(err.Error(), "设置 › 模型配置") {
		t.Fatalf("unconfigured error: %v", err)
	}

	// Keyless, hidden, Responses and model-less launchers are not candidates; a keyed Messages launcher is.
	launchers := []storedLauncher{
		{ManagedLauncher: proto.ManagedLauncher{Name: "nokey", BaseURL: srv.URL, Model: "m"}},
		{ManagedLauncher: proto.ManagedLauncher{Name: "hidden", BaseURL: srv.URL, Model: "m"}, KeySealed: key},
		{ManagedLauncher: proto.ManagedLauncher{Name: "responses", API: "responses", BaseURL: srv.URL, Model: "m"}, KeySealed: key},
		{ManagedLauncher: proto.ManagedLauncher{Name: "nomodel", BaseURL: srv.URL}, KeySealed: key},
		{ManagedLauncher: proto.ManagedLauncher{Name: "claude-gw", BaseURL: srv.URL, Model: "gateway-default"}, KeySealed: key},
	}
	if err := h.put(ctx, launchersManagedKey, launchers); err != nil {
		t.Fatal(err)
	}
	if err := h.put(ctx, launchersHiddenKey, []string{"hidden"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"inbox.triage", "inbox.judge", "inbox.draft", "translate", "kb.compress", "assistant.title", "agent.title"} {
		p := purpose(id)
		if p.Effective.Provider != "launcher.claude-gw" || p.Effective.Model != "gateway-default" || p.Status != "未验证" {
			t.Fatalf("%s did not fall back: %+v", id, p)
		}
		if _, err := h.purposeModel(id).Complete(ctx, "", "", 10); err != nil || model != "gateway-default" {
			t.Fatalf("%s: sent %q, %v", id, model, err)
		}
	}
	if p := purpose("kb"); p.InheritedFrom != modelAutoSource {
		t.Fatalf("fallback not labelled: %+v", p)
	}
	if p := purpose("translate"); p.InheritedFrom != "信息处理" {
		t.Fatalf("child no longer inherits its parent: %+v", p)
	}

	// A direct API provider comes before a launcher; nothing is written as an override.
	launchers = append(launchers, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "direct", Kind: "api", API: "responses", BaseURL: srv.URL, Model: "api-default"}, KeySealed: key})
	if err := h.put(ctx, launchersManagedKey, launchers); err != nil {
		t.Fatal(err)
	}
	if b, _ := h.resolveModel(ctx, "inbox.draft"); b.Provider != "launcher.direct" {
		t.Fatalf("API provider not preferred: %+v", b)
	}
	if r, _ := h.modelRecord(ctx, "inbox.draft"); r.Revision != 0 || r.Binding != (ModelBinding{}) {
		t.Fatalf("fallback was stored: %+v", r)
	}

	// An explicit choice wins over the fallback, and hub.json's "ai" still wins over it too.
	if r := modelRequest(mux, "PATCH", "kb", `{"revision":0,"provider":"launcher.claude-gw","model":"picked"}`); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if b, _ := h.resolveModel(ctx, "inbox.draft"); b.Provider != "launcher.claude-gw" || b.Model != "picked" {
		t.Fatalf("explicit choice lost: %+v", b)
	}
	h.cfg.AI = ai.Config{BaseURL: srv.URL, APIKeyFile: "/tmp/test-key", Model: "hub-json"}
	if b, _ := h.resolveModel(ctx, "inbox.triage"); b.Provider != "legacy.triage" || b.Model != "hub-json" {
		t.Fatalf("hub.json baseline replaced: %+v", b)
	}
}
