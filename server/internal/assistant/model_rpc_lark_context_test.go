package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const larkContextCompleteParams = `{"messages":[{"role":"user","content":"Summarize this."}]}`

func TestLarkContextModelCompleteUsesOnlyItsDirectBinding(t *testing.T) {
	for _, kind := range []string{"api", "launcher"} {
		t.Run(kind, func(t *testing.T) { testLarkContextDirectBinding(t, kind) })
	}
}

func testLarkContextDirectBinding(t *testing.T, kind string) {
	t.Helper()
	s, f, kbCalls := extensionModelService(t, "unexpected KB reply")
	f.modelExts = append(f.modelExts, assistantwire.ModelExtension{ID: "current-context", StorageID: "lark-context", Name: "Context"})
	var requests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Model  string            `json:"model"`
			Output map[string]string `json:"output_config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "Bearer fixture-key" ||
			body.Model != "fixture-opus" || body.Output["effort"] != "xhigh" {
			t.Errorf("unexpected gateway route or effective model settings: %s %s %+v", r.Method, r.URL.Path, body)
		}
		fmt.Fprint(w, `{"model":"fixture-opus","stop_reason":"end_turn","content":[{"type":"text","text":"{\"summary\":\"done\"}"}],"usage":{"input_tokens":5,"output_tokens":3}}`)
	}))
	t.Cleanup(gateway.Close)
	seal, err := s.sealKey("fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	putLaunchers(t, s, storedLauncher{KeySealed: seal, ManagedLauncher: proto.ManagedLauncher{
		Name: "selected-api", Kind: kind, BaseURL: gateway.URL, Auth: "bearer", Model: "fixture-default", OpusModel: "fixture-opus", Effort: "low",
	}})
	if err := s.writeModelRecord(t.Context(), larkContextCompletePurpose, 0,
		ModelBinding{Provider: "launcher.selected-api", Model: "Opus[1m]", Effort: "xhigh"},
		&candidateSetting{Mode: "custom", Custom: []ModelBinding{{Provider: "launcher.selected-api", Model: "substitute"}}}, true); err != nil {
		t.Fatal(err)
	}
	params := `{"messages":[{"role":"user","content":"Summarize this."}],"models":["another-model"],"json_schema":{"type":"object","required":["summary"]}}`
	out, err := completeLarkContextAs(t, s, "current-context", "lark-context", params)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(assistantwire.ModelCompleteResult)
	if res.Model != "fixture-opus" || string(res.JSON) != `{"summary":"done"}` || res.Candidate != "" || len(res.Attempts) != 0 ||
		res.Usage.Requests != 1 || res.Usage.InputTokens != 5 || requests.Load() != 1 || len(kbCalls()) != 0 {
		t.Fatalf("reply %+v, requests %d, KB calls %d", res, requests.Load(), len(kbCalls()))
	}
}

func TestLarkContextModelCompleteDirectFailureDoesNotReplayOrLeak(t *testing.T) {
	for _, tc := range []struct {
		name, api, code string
		respond         func(http.ResponseWriter, *http.Request)
	}{
		{"invalid schema output", "messages", assistantwire.CodeInvalidOutput, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"content":[{"type":"text","text":"{\"fixture-key\":true}"}],"stop_reason":"end_turn"}`)
		}},
		{"chat compatibility rejection", "chat", assistantwire.CodeModelFailed, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead. %s %s"}}`, r.Host, r.Header.Get("Authorization"))
		}},
		{"redirect", "messages", assistantwire.CodeModelFailed, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/other-endpoint")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}},
		{"quota pause", "messages", assistantwire.CodeModelPaused, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("X-Should-Retry", "false")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"quota exhausted for fixture-key"}}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, kbCalls := extensionModelService(t, "unexpected KB reply")
			f.modelExts = []assistantwire.ModelExtension{{ID: "lark-context", Name: "Context"}}
			var requests atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				tc.respond(w, r)
			}))
			t.Cleanup(gateway.Close)
			key, err := s.sealKey("fixture-key")
			if err != nil {
				t.Fatal(err)
			}
			putLaunchers(t, s, storedLauncher{KeySealed: key, ManagedLauncher: proto.ManagedLauncher{
				Name: "selected-api", Kind: "api", API: tc.api, BaseURL: gateway.URL, Model: "fixture-model",
			}})
			setModel(t, s, larkContextCompletePurpose, ModelBinding{Provider: "launcher.selected-api"})
			_, err = completeLarkContextAs(t, s, "lark-context", "", `{"messages":[{"role":"user","content":"hi"}],"json_schema":{"type":"object","additionalProperties":false}}`)
			var re *rpc.Error
			if !errors.As(err, &re) || re.Code != tc.code || requests.Load() != 1 || len(kbCalls()) != 0 {
				t.Fatalf("error %v, direct requests %d, KB calls %d", err, requests.Load(), len(kbCalls()))
			}
			raw, _ := json.Marshal(re)
			for _, secret := range []string{"fixture-key", strings.TrimPrefix(gateway.URL, "http://"), "/other-endpoint"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("RPC error exposed private gateway detail: %s", raw)
				}
			}
		})
	}
}

func TestLarkContextModelCompleteReturnsTheExactLocalBinding(t *testing.T) {
	for _, tc := range []struct {
		name, runtime, advertisedModel, model, effectiveModel, effort string
	}{
		{"agy", "agy", "gemini-pro", "", "gemini-pro", "high"},
		{"work-codex", "codex", "default-model", "gpt-5.6-sol", "gpt-5.6-sol", "ultra"},
		{"traex", "traex", "default-model", "gpt-5.6-sol", "gpt-5.6-sol", "xhigh"},
		{"claude", "claude", "", "Opus[1m]", "Opus[1m]", "max"},
		{"codex", "codex", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, calls := extensionModelService(t, "unexpected KB reply")
			f.modelExts = []assistantwire.ModelExtension{{ID: "lark-context", Name: "Context"}}
			f.launcherList = []proto.Launcher{{Name: tc.name, Agent: tc.runtime, Model: tc.advertisedModel, BaseURL: "https://gateway.example.test/private"}}
			provider := "launcher." + tc.name
			setModel(t, s, larkContextCompletePurpose, ModelBinding{Provider: provider, Model: tc.model, Effort: tc.effort})
			_, err := completeAs(s, "lark-context", `{"messages":[{"role":"user","content":"hi"}],"models":["unapproved-model"]}`)
			var re *rpc.Error
			want := fmt.Sprintf(`{"binding":{"provider":%q,"runtime":%q,"model":%q,"effort":%q}}`, provider, tc.runtime, tc.effectiveModel, tc.effort)
			if !errors.As(err, &re) || re.Code != assistantwire.CodeModelLocalLauncherRequired || string(re.Data) != want ||
				strings.Contains(re.Message, "example.test") || len(calls()) != 0 {
				t.Fatalf("wanted %s, got %+v, KB calls %d", want, re, len(calls()))
			}
		})
	}
}

func TestLarkContextModelCompleteUnavailableAndManagedBoundaries(t *testing.T) {
	s, f, kbCalls := extensionModelService(t, "unexpected KB reply")
	f.modelExts = []assistantwire.ModelExtension{{ID: "lark-context", Name: "Context"}}
	f.launcherList = []proto.Launcher{{Name: "codex", Agent: "codex"}, {Name: "omp", Agent: "omp"}}
	key, err := s.sealKey("fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	putLaunchers(t, s,
		storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "codex", Agent: "codex"}},
		storedLauncher{KeySealed: key, EnvSealed: "fixture-private-env", ManagedLauncher: proto.ManagedLauncher{
			Name: "with-settings", Kind: "launcher", BaseURL: f.hello.Models.AI.BaseURL, Model: "fixture-model",
		}},
		storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "missing-key", Kind: "api", BaseURL: f.hello.Models.AI.BaseURL, Model: "fixture-model"}})
	for _, tc := range []struct{ provider, message string }{
		{"launcher.codex", "managed launcher"},
		{"launcher.with-settings", "managed launcher"},
		{"launcher.missing-key", "direct API could not be used"},
		{"launcher.omp", "unavailable or unsupported"},
		{"launcher.missing", "unavailable or unsupported"},
		{"legacy.unknown", "provider is unavailable"},
	} {
		setModel(t, s, larkContextCompletePurpose, ModelBinding{Provider: tc.provider})
		_, err := completeAs(s, "lark-context", larkContextCompleteParams)
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != assistantwire.CodeModelUnavailable || len(re.Data) != 0 || !strings.Contains(re.Message, tc.message) {
			t.Fatalf("%s: %v", tc.provider, err)
		}
	}
	if len(kbCalls()) != 0 {
		t.Fatal("unavailable providers fell back to KB")
	}
}

func TestLarkContextModelCompleteUsesCanonicalStorageScope(t *testing.T) {
	s, f, kbCalls := extensionModelService(t, "KB reply")
	// A display/current ID identical to the special purpose does not confer its canonical storage ownership.
	f.modelExts = []assistantwire.ModelExtension{{ID: "lark-context", StorageID: "another-owner", Name: "Example"}}
	asked := candidateGateways(t, s, map[string]string{"candidate": "candidate reply"})
	out, err := completeLarkContextAs(t, s, "lark-context", "another-owner", `{"messages":[{"role":"user","content":"hi"}],"models":["candidate"]}`)
	if err != nil || out.(assistantwire.ModelCompleteResult).Candidate != "candidate" || !slices.Equal(asked(), []string{"candidate"}) || len(kbCalls()) != 0 {
		t.Fatalf("unrelated purpose: %+v, %v, candidate requests %v", out, err, asked())
	}
	_, err = completeAs(s, "lark-context", larkContextCompleteParams)
	if rpcCode(err) != assistantwire.CodeModelUnavailable {
		t.Fatalf("unregistered canonical owner: %v", err)
	}
	_, err = completeLarkContextAs(t, s, "lark-context", "another-owner", `{"messages":[{"role":"user","content":"hi"}],"storage_id":"lark-context"}`)
	if rpcCode(err) != bus.CodeInvalidParams {
		t.Fatalf("program supplied a storage owner: %v", err)
	}
}

func completeLarkContextAs(t *testing.T, s *Service, extension, storageID, input string) (any, error) {
	t.Helper()
	raw, err := json.Marshal(assistantwire.ExtensionComplete{Extension: extension, StorageID: storageID, Params: json.RawMessage(input)})
	if err != nil {
		t.Fatal(err)
	}
	out, handled, err := s.modelsRequest(context.Background(), assistantwire.MethodModelsExtensionComplete, raw)
	if !handled {
		t.Fatal("extension model request was not handled")
	}
	return out, err
}
