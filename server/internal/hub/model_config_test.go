package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/proto"
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

func TestModelConfigurationPluginFailureIsVisible(t *testing.T) {
	h, mux := modelTestHub(t)
	h.plugins = newPlugins(t.TempDir(), []PluginConfig{{ID: "voice", Name: "Voice", Kind: "voice", Socket: "/nonexistent/model-test.sock"}}, t.Logf)
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
	if result.Purpose.Status != "读不到插件模型列表" {
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
	r = modelRequest(mux, "PATCH", "inbox.judge", `{"revision":0,"model":"judge"}`)
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
	h.cfg.LarkAgent.Launcher = "slash"
	h.cfg.Inbox.Agent.Launcher = "delegate"
	for id, want := range map[string]string{"inbox.triage": "triage", "inbox.judge": "judge", "translate": "triage", "kb.compress": "knowledge", "assistant.title": "base"} {
		b, err := h.resolveModel(ctx, id)
		if err != nil || b.Model != want {
			t.Errorf("%s: %+v %v", id, b, err)
		}
	}
	for id, want := range map[string]string{"assistant": "conversation", "lark.agent": "slash", "inbox.agent": "delegate"} {
		b, err := h.resolveModel(ctx, id)
		if err != nil || b.Provider != launcherProvider(want) {
			t.Errorf("%s: %+v %v", id, b, err)
		}
	}
	if err := h.saveModelRecord(ctx, "lark.agent", 0, ModelBinding{Provider: "launcher.new"}); err != nil {
		t.Fatal(err)
	}
	b, _ := h.resolveModel(ctx, "lark.agent")
	if b.Provider != "launcher.new" {
		t.Fatal("old slash config overrode new selection")
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
	for _, p := range modelPurposes {
		if p.Kind != "api" {
			continue
		}
		r := modelRequest(mux, "PATCH", p.ID, `{"revision":0,"provider":"launcher.direct","model":"`+p.ID+`"}`)
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
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	s.tasks = []Task{{ID: "one", Title: "任务", Prompt: "x", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true}}
	s.mu.Unlock()
	r := modelRequest(mux, "PATCH", "task.one", `{"revision":0,"provider":"launcher.claude","model":"exact","effort":"high"}`)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	h.scheduleRoutes(mux)
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
