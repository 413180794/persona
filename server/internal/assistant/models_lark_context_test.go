package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func larkContextModelGrant(f *fakeHub, granted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modelExts = []assistantwire.ModelExtension{{ID: "fixture-notes", Name: "Fixture Notes"}}
	if granted {
		f.modelExts = append(f.modelExts, assistantwire.ModelExtension{ID: "lark-context", Name: "Lark Context"})
	}
}

func larkContextStartRequest(s *Service, purpose string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/conversations", strings.NewReader(
		fmt.Sprintf(`{"purpose":%q,"prompt":"Synthetic Lark shortcut"}`, purpose)))
	r.Header.Set(RoleHeader, roleExtension)
	r.Header.Set(ExtensionHeader, "fixture-lark")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func assertLarkContextNativeStart(t *testing.T, s *Service, f *fakeHub, purpose string, want ModelBinding) {
	t.Helper()
	r := larkContextStartRequest(s, purpose)
	if r.Code != http.StatusOK {
		t.Fatalf("start %s: %d %s", purpose, r.Code, r.Body.String())
	}
	var out struct{ Session proto.Session }
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	send := f.sends[len(f.sends)-1]
	f.mu.Unlock()
	launcher := strings.TrimPrefix(want.Provider, "launcher.")
	if send["launcher"] != launcher || send["model"] != want.Model || send["effort"] != want.Effort ||
		send["launch"] != "headless" || send["model_fingerprint"] == "" || out.Session.SessionID == "" ||
		out.Session.Launcher != launcher || out.Session.Model != want.Model {
		t.Fatalf("native selection: send=%+v session=%+v, want %+v", send, out.Session, want)
	}
	if purpose == purposeLarkAgent && (send["origin"] != OriginLarkAgent || out.Session.Origin != OriginLarkAgent) {
		t.Fatalf("shortcut lost its origin: send=%+v session=%+v", send, out.Session)
	}
	t.Logf("native %s -> assistant.send launcher=%s model=%s effort=%s", purpose, send["launcher"], send["model"], send["effort"])
}

func TestLarkContextModelSettingsAndLegacyRecords(t *testing.T) {
	s, f, st := modelService(t)
	ctx := context.Background()
	larkContextModelGrant(f, true)
	main := ModelBinding{Provider: "launcher.claude", Model: "main-before", Effort: "high"}
	agents := ModelBinding{Provider: "launcher.claude", Model: "agents-default", Effort: "medium"}
	setModel(t, s, "assistant", main)
	setModel(t, s, "agent.new", agents)
	oldAPI := modelRecord{Revision: 12, Binding: ModelBinding{Provider: "launcher.old-responses", Model: "old-completion"},
		Candidates: &candidateSetting{Mode: candidatesOff}}
	oldNative := modelRecord{Revision: 23, Binding: ModelBinding{Provider: "launcher.claude", Model: "old-shortcut", Effort: "low"}}
	oldRetired := modelRecord{Revision: 34, Binding: ModelBinding{Provider: "launcher.retired", Model: "retired"}}
	oldRecords := map[string]modelRecord{
		modelKey(larkContextModelPurposeID): oldAPI, modelRecordKey(purposeLarkAgent): oldNative, modelKey(purposeLarkAgent): oldRetired,
	}
	for key, r := range oldRecords {
		if err := st.PutSetting(ctx, key, r); err != nil {
			t.Fatal(err)
		}
	}
	catalogHas := func(canonical, legacy bool) {
		t.Helper()
		w := modelRequest(s, http.MethodGet, "", "")
		var out struct{ Purposes []ModelPurpose }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
		}
		counts := map[string]int{}
		for _, p := range out.Purposes {
			counts[p.ID]++
		}
		if counts[larkContextModelPurposeID] > 1 || counts[purposeLarkAgent] > 1 ||
			(counts[larkContextModelPurposeID] == 1) != canonical || (counts[purposeLarkAgent] == 1) != legacy {
			t.Fatalf("catalog: %v", counts)
		}
	}
	assertCanonical := func(want ModelBinding, revision int64, override ModelBinding) {
		t.Helper()
		p := purposeOf(t, s, larkContextModelPurposeID)
		if p.ID != larkContextModelPurposeID || p.Parent != "assistant" || p.CategoryID != "assistant" || p.Kind != "launcher" ||
			p.Launch != "headless" || p.Name != "Lark Context" || p.Candidates != nil || p.Revision != revision || p.Override != override || p.Effective != want ||
			!slices.Equal(p.Fields, []string{"provider", "model", "effort"}) || (p.InheritedFrom != "") != (override.Provider == "") {
			t.Fatalf("canonical purpose: %+v, want binding %+v revision %d override %+v", p, want, revision, override)
		}
		for _, id := range []string{larkContextModelPurposeID, purposeLarkAgent} {
			if got, err := s.resolveModel(ctx, id); err != nil || got != want {
				t.Fatalf("resolve %s: %+v %v, want %+v", id, got, err, want)
			}
		}
	}

	catalogHas(true, false)
	assertCanonical(main, 0, ModelBinding{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch} {
		if r := modelRequest(s, method, purposeLarkAgent, `{"revision":23,"model":"unexpected"}`); r.Code != http.StatusNotFound {
			t.Fatalf("legacy settings %s: %d %s", method, r.Code, r.Body.String())
		}
	}
	if p := purposeOf(t, s, "extension.fixture-notes"); p.Parent != "kb" || p.CategoryID != "kb" || p.Kind != "api" {
		t.Fatalf("unrelated extension changed: %+v", p)
	}
	assertLarkContextNativeStart(t, s, f, purposeLarkAgent, main)
	if def := s.launcherDefault(ctx, purposeLarkAgent); def["model"] != main.Model || def["effort"] != main.Effort {
		t.Fatalf("legacy picker default: %v", def)
	}

	putLaunchers(t, s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "shortcut-claude", Agent: "claude"}})
	writes := []struct {
		method, body string
		code         int
	}{
		{http.MethodPatch, `{"revision":0,"candidates":{"mode":"off"}}`, http.StatusBadRequest},
		{http.MethodPut, `{"revision":0,"provider":"launcher.shortcut-claude","model":"separately-picked","effort":"medium"}`, http.StatusOK},
		{http.MethodPatch, `{"revision":1,"effort":"xhigh"}`, http.StatusOK},
		{http.MethodPatch, `{"revision":1,"model":"stale"}`, http.StatusConflict},
	}
	for _, w := range writes {
		if r := modelRequest(s, w.method, larkContextModelPurposeID, w.body); r.Code != w.code {
			t.Fatalf("%s %s: %d %s", w.method, w.body, r.Code, r.Body.String())
		}
	}
	chosen := ModelBinding{Provider: "launcher.shortcut-claude", Model: "separately-picked", Effort: "xhigh"}
	s = newServiceOn(t, f, st)
	assertCanonical(chosen, 2, chosen)
	assertLarkContextNativeStart(t, s, f, purposeLarkAgent, chosen)
	assertLarkContextNativeStart(t, s, f, larkContextModelPurposeID, chosen)
	assertLarkContextNativeStart(t, s, f, purposeLarkContextAgent, chosen)
	if uses, err := s.providerModelUses(ctx, chosen.Provider); err != nil || !slices.Equal(uses, []string{"Lark Context"}) {
		t.Fatalf("provider use must count the canonical assignment once: %v %v", uses, err)
	}
	if cur, err := s.resolveModel(ctx, "assistant"); err != nil || cur != main {
		t.Fatalf("main chat changed: %+v %v", cur, err)
	}
	if cur, err := s.resolveModel(ctx, "inbox.agent"); err != nil || cur != agents {
		t.Fatalf("AI twin changed: %+v %v", cur, err)
	}

	if r := modelRequest(s, http.MethodPut, larkContextModelPurposeID, `{"revision":2,"provider":null,"model":null,"effort":null}`); r.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", r.Code, r.Body.String())
	}
	main.Model, main.Effort = "main-after", "max"
	setModel(t, s, "assistant", main)
	assertCanonical(main, 3, ModelBinding{})
	assertLarkContextNativeStart(t, s, f, purposeLarkAgent, main)

	larkContextModelGrant(f, false)
	catalogHas(false, true)
	if r := modelRequest(s, http.MethodGet, larkContextModelPurposeID, ""); r.Code != http.StatusNotFound {
		t.Fatalf("canonical purpose without grant: %d %s", r.Code, r.Body.String())
	}
	if p := purposeOf(t, s, purposeLarkAgent); p.Parent != "agent.new" || p.Revision != oldNative.Revision || p.Effective != oldNative.Binding {
		t.Fatalf("upstream binding after disabling: %+v", p)
	}
	assertLarkContextNativeStart(t, s, f, purposeLarkAgent, oldNative.Binding)
	larkContextModelGrant(f, true)
	assertCanonical(main, 3, ModelBinding{})
	for key, want := range oldRecords {
		var got modelRecord
		if found, err := st.Setting(ctx, key, &got); err != nil || !found || !reflect.DeepEqual(got, want) {
			t.Fatalf("old record %s changed: %+v, %v (found %t), want %+v", key, got, err, found, want)
		}
	}
}

func TestLarkContextModelDirectTransports(t *testing.T) {
	for _, tc := range []struct{ api, endpoint, key, event string }{
		{"messages", "/v1/messages", "output_config", `{"content":[{"type":"text","text":"ok"}]}`},
		{"responses", "/v1/responses", "reasoning", `data: {"type":"response.output_text.delta","delta":"ok"}

data: {"type":"response.completed"}

`},
		{"chat", "/v1/chat/completions", "reasoning_effort", `data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}

data: [DONE]

`},
	} {
		t.Run(tc.api, func(t *testing.T) {
			s, f, _ := modelService(t)
			larkContextModelGrant(f, true)
			const model, effort = "override-model", "high"
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				got := body[tc.key]
				if nested, ok := got.(map[string]any); ok {
					got = nested["effort"]
				}
				if r.Method != http.MethodPost || r.URL.Path != tc.endpoint || body["model"] != model || got != effort {
					t.Errorf("direct request %s %s %+v", r.Method, r.URL.Path, body)
				}
				requests <- r.URL.Path
				if tc.api != "messages" {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				fmt.Fprint(w, tc.event)
			}))
			defer server.Close()
			key, err := s.sealKey("fixture-key")
			if err != nil {
				t.Fatal(err)
			}
			provider := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "fixture-api", Kind: "api", API: tc.api,
				BaseURL: server.URL, Model: "provider-default"}, KeySealed: key}
			if tc.api != "messages" {
				provider.Share = shareNone // manager-only connections still belong in the editor
			}
			putLaunchers(t, s, provider)
			want := ModelBinding{Provider: "launcher.fixture-api", Model: model, Effort: effort}
			patch := fmt.Sprintf(`{"revision":0,"provider":%q,"model":%q,"effort":%q}`, want.Provider, want.Model, want.Effort)
			if r := modelRequest(s, http.MethodPatch, larkContextModelPurposeID, patch); r.Code != http.StatusOK {
				t.Fatalf("select %s: %d %s", tc.api, r.Code, r.Body.String())
			}
			if p := purposeOf(t, s, larkContextModelPurposeID); p.Effective != want || !slices.Contains(p.ProviderIDs, want.Provider) {
				t.Fatalf("direct binding: %+v", p)
			}
			reply, err := s.purposeModel(larkContextModelPurposeID).Chat(context.Background(), ai.Request{
				Messages: []ai.Message{{Role: ai.RoleUser, Content: "fixture"}}, MaxTokens: 32,
			})
			if err != nil || reply.Text != "ok" || len(requests) != 1 {
				t.Fatalf("direct completion: %+v %v (%d requests)", reply, err, len(requests))
			}
			t.Logf("direct %s -> %s model=%s effort=%s", tc.api, <-requests, model, effort)
			if tc.api == "messages" {
				assertLarkContextNativeStart(t, s, f, purposeLarkAgent, want)
			} else {
				w := larkContextStartRequest(s, purposeLarkAgent)
				if w.Code < 400 {
					t.Fatalf("native start accepted unsupported protocol: %d %s", w.Code, w.Body.String())
				}
				f.mu.Lock()
				sends := len(f.sends)
				f.mu.Unlock()
				if sends != 0 || len(requests) != 0 {
					t.Fatalf("native start unexpectedly dispatched: %d node calls, %d HTTP requests", sends, len(requests))
				}
				t.Logf("native %s refused before assistant.send: HTTP %d", tc.api, w.Code)
			}
		})
	}
}

func TestLarkContextModelNonClaudeLaunchers(t *testing.T) {
	s, f, st := modelService(t)
	larkContextModelGrant(f, true)
	launchers := []proto.Launcher{
		{Name: "codex", Agent: "codex", Source: "zshrc"},
		{Name: "agy", Agent: "agy", Source: "agy", Assistant: true},
		{Name: "traex", Agent: "traex", Source: "zshrc"},
	}
	f.mu.Lock()
	f.zshrc = map[string][]proto.Launcher{"server": launchers}
	f.mu.Unlock()
	if err := st.PutSetting(context.Background(), launchersZshrcKey, map[string][]proto.Launcher{"server": launchers}); err != nil {
		t.Fatal(err)
	}
	main := purposeOf(t, s, "assistant")
	if slices.Contains(main.ProviderIDs, "launcher.traex") {
		t.Fatalf("ordinary TraeX was added to Main Chat: %+v", main.ProviderIDs)
	}
	for rev, tc := range []struct {
		launcher, model, effort string
		native                  bool
	}{
		{"codex", "fixture-codex", "ultra", true},
		{"agy", "fixture-agy", "high", true},
		{"traex", "fixture-traex", "xhigh", false},
	} {
		t.Run(tc.launcher, func(t *testing.T) {
			want := ModelBinding{Provider: launcherProvider(tc.launcher), Model: tc.model, Effort: tc.effort}
			patch := fmt.Sprintf(`{"revision":%d,"provider":%q,"model":%q,"effort":%q}`, rev, want.Provider, want.Model, want.Effort)
			if r := modelRequest(s, http.MethodPatch, larkContextModelPurposeID, patch); r.Code != http.StatusOK {
				t.Fatalf("save: %d %s", r.Code, r.Body.String())
			}
			if p := purposeOf(t, s, larkContextModelPurposeID); p.Effective != want || !slices.Contains(p.ProviderIDs, want.Provider) {
				t.Fatalf("configured binding: %+v", p)
			}
			if tc.native {
				assertLarkContextNativeStart(t, s, f, purposeLarkAgent, want)
				return
			}
			f.mu.Lock()
			before := len(f.sends)
			f.mu.Unlock()
			w := larkContextStartRequest(s, purposeLarkAgent)
			f.mu.Lock()
			after := len(f.sends)
			f.mu.Unlock()
			if w.Code < 400 || before != after {
				t.Fatalf("ordinary TraeX unexpectedly started: HTTP %d %s, sends %d -> %d", w.Code, w.Body.String(), before, after)
			}
			t.Logf("native TraeX refused before assistant.send: HTTP %d", w.Code)
		})
	}
}
