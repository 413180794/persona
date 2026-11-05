package hub

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

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Fake aliases only: no real gateway, key or header.
const (
	importKeyA   = "sk-test-import-a"
	importKeyB   = "sk-test-import-b"
	importHeader = "X-Test-Secret: hdr-test-value"
)

func TestMergeZshrcAliases(t *testing.T) {
	gw := func(alias string, env map[string]string) zshrcAlias {
		e := map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com/", "ANTHROPIC_AUTH_TOKEN": "Bearer " + importKeyA}
		for k, v := range env {
			e[k] = v
		}
		return zshrcAlias{Alias: alias, Env: e, Command: "claude"}
	}
	cases := []struct {
		name    string
		aliases []zshrcAlias
		known   []knownProvider
		want    []importGroup
	}{
		{
			name: "one alias with every setting",
			aliases: []zshrcAlias{{Alias: "gcc", Command: "claude", Args: []string{"--dangerously-skip-permissions"}, Env: map[string]string{
				"ANTHROPIC_BASE_URL": "https://gw.example.com", "ANTHROPIC_API_KEY": importKeyA, "ANTHROPIC_MODEL": "glm-5[1m]",
				"ANTHROPIC_DEFAULT_OPUS_MODEL": "glm-5[1m]", "ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-air", "ANTHROPIC_SMALL_FAST_MODEL": "glm-air",
				"CLAUDE_CODE_SUBAGENT_MODEL": "glm-5", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE": "1", "CLAUDE_CODE_EFFORT_LEVEL": "high",
				"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "180000", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000", "API_TIMEOUT_MS": "600000",
				"ANTHROPIC_CUSTOM_HEADERS": importHeader, "HTTP_PROXY": "http://proxy.example.com:1", "NO_PROXY": "localhost",
			}}},
			want: []importGroup{{key: importKeyA, headers: importHeader, importedProvider: importedProvider{
				HasKey: true, Aliases: []string{"gcc"},
				ManagedLauncher: proto.ManagedLauncher{Name: "gcc", BaseURL: "https://gw.example.com", Auth: "api_key", Model: "glm-5",
					Args: []string{"--dangerously-skip-permissions"}, Models: []proto.ProviderModel{{ID: "glm-5", ContextWindow: 1000000}},
					OpusModel: "glm-5", HaikuModel: "glm-air", SmallModel: "glm-air", SubagentModel: "glm-5", SubagentForce: true,
					Effort: "high", CompactWindow: 180000, MaxContext: 1000000, TimeoutMS: 600000, HasHeaders: true,
					Proxy: "http://proxy.example.com:1", NoProxy: "localhost", Source: "zshrc:gcc@m1"},
			}}},
		},
		{
			name: "same gateway and key merge their models; the first alias names it",
			aliases: []zshrcAlias{
				gw("kimi", map[string]string{"ANTHROPIC_MODEL": "kimi-k3"}),
				gw("kimi2", map[string]string{"ANTHROPIC_MODEL": "kimi-k3[1m]"}),
				gw("kimi3", map[string]string{"ANTHROPIC_MODEL": "kimi-flash"}),
			},
			known: []knownProvider{{name: "stored", baseURL: "https://gw.example.com", key: importKeyA}},
			want: []importGroup{{key: importKeyA, importedProvider: importedProvider{
				HasKey: true, Aliases: []string{"kimi", "kimi2", "kimi3"}, Existing: "stored",
				ManagedLauncher: proto.ManagedLauncher{Name: "kimi", BaseURL: "https://gw.example.com", Auth: "bearer", Model: "kimi-k3",
					Models: []proto.ProviderModel{{ID: "kimi-k3", ContextWindow: 1000000}, {ID: "kimi-flash"}}, Source: "zshrc:kimi@m1"},
			}}},
		},
		{
			name: "another key is another provider; plain claude, codex and bad names",
			aliases: []zshrcAlias{
				gw("a.cc", map[string]string{"ANTHROPIC_MODEL": "m"}),
				gw("1cc", map[string]string{"ANTHROPIC_AUTH_TOKEN": importKeyB}),
				{Alias: "plain", Command: "claude", Env: map[string]string{"ANTHROPIC_MODEL": "opus"}},
				{Alias: "cx", Command: "codex", Env: map[string]string{"ANTHROPIC_BASE_URL": "https://x.example.com"}},
			},
			known: []knownProvider{{name: "other-key", baseURL: "https://gw.example.com", key: importKeyB + "-no"}},
			want: []importGroup{
				{key: importKeyA, importedProvider: importedProvider{HasKey: true, Aliases: []string{"a.cc"}, ManagedLauncher: proto.ManagedLauncher{
					Name: "a-cc", BaseURL: "https://gw.example.com", Auth: "bearer", Model: "m", Models: []proto.ProviderModel{{ID: "m"}}, Source: "zshrc:a.cc@m1"}}},
				{key: importKeyB, importedProvider: importedProvider{HasKey: true, Aliases: []string{"1cc"}, ManagedLauncher: proto.ManagedLauncher{
					Name: "zshrc_1cc", BaseURL: "https://gw.example.com", Auth: "bearer", Source: "zshrc:1cc@m1"}}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeZshrcAliases("m1", c.aliases, c.known)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestMergeImportedModelsAddsOnlyNew(t *testing.T) {
	models := []proto.ProviderModel{{ID: "a", Name: "A", Efforts: []string{"high"}}, {ID: "b", ContextWindow: 200000}}
	if !mergeImportedModels(&models, []proto.ProviderModel{{ID: "a", ContextWindow: 1000000}, {ID: "b"}, {ID: "c"}}) {
		t.Fatal("no change reported")
	}
	want := []proto.ProviderModel{{ID: "a", Name: "A", Efforts: []string{"high"}, ContextWindow: 1000000}, {ID: "b", ContextWindow: 200000}, {ID: "c"}}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("%+v", models)
	}
	if mergeImportedModels(&models, []proto.ProviderModel{{ID: "c"}}) {
		t.Fatal("a change reported for nothing new")
	}
}

func importTestHub(t *testing.T, aliases []zshrcAlias, err error) (*Hub, *http.ServeMux) {
	h, _ := modelTestHub(t)
	old := zshrcAliases
	zshrcAliases = func(context.Context, *Hub, string) ([]zshrcAlias, error) { return aliases, err }
	t.Cleanup(func() { zshrcAliases = old })
	mux := http.NewServeMux()
	h.launcherImportRoutes(mux)
	return h, mux
}

func importRequest(mux http.Handler, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(method, "/api/v1/launchers/import?machine=m1", strings.NewReader(body)))
	return r
}

var importAliases = []zshrcAlias{
	{Alias: "gcc", Command: "claude", Env: map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com", "ANTHROPIC_API_KEY": importKeyA,
		"ANTHROPIC_MODEL": "glm-5", "ANTHROPIC_CUSTOM_HEADERS": importHeader}},
	{Alias: "gcc2", Command: "claude", Env: map[string]string{"ANTHROPIC_BASE_URL": "https://gw.example.com", "ANTHROPIC_API_KEY": importKeyA,
		"ANTHROPIC_MODEL": "glm-air"}},
	{Alias: "kcc", Command: "claude", Env: map[string]string{"ANTHROPIC_BASE_URL": "https://k.example.com", "ANTHROPIC_AUTH_TOKEN": importKeyB,
		"ANTHROPIC_MODEL": "kimi-k3[1m]"}},
}

func TestLauncherImportPreviewHasNoSecrets(t *testing.T) {
	_, mux := importTestHub(t, importAliases, nil)
	r := importRequest(mux, "GET", "")
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	body := r.Body.String()
	for _, secret := range []string{importKeyA, importKeyB, "hdr-test-value", `"api_key":`, `"headers":`} {
		if strings.Contains(body, secret) {
			t.Fatalf("the preview carries %q: %s", secret, body)
		}
	}
	var out struct {
		Machine   string             `json:"machine"`
		Providers []importedProvider `json:"providers"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	if out.Machine != "m1" || len(out.Providers) != 2 || !out.Providers[0].HasKey || !out.Providers[0].HasHeaders ||
		len(out.Providers[0].Models) != 2 || out.Providers[1].Models[0].ContextWindow != 1000000 {
		t.Fatalf("%+v", out)
	}
}

func TestLauncherImportCommitSealsAndMergesExisting(t *testing.T) {
	h, mux := importTestHub(t, importAliases, nil)
	ctx := context.Background()
	sealed, err := h.sealKey(importKeyB)
	if err != nil {
		t.Fatal(err)
	}
	existing := storedLauncher{KeySealed: sealed, ManagedLauncher: proto.ManagedLauncher{Name: "kimi", BaseURL: "https://k.example.com",
		Model: "kept", Effort: "low", Models: []proto.ProviderModel{{ID: "kept"}}}}
	taken := storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "gcc", BaseURL: "https://elsewhere.example.com", Model: "x"}}
	if err := h.put(ctx, launchersManagedKey, []storedLauncher{existing, taken}); err != nil {
		t.Fatal(err)
	}
	r := importRequest(mux, "POST", `{"machine":"m1","providers":["gcc","kcc"]}`)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	if want := `{"created":["gcc-2"],"updated":["kimi"]}`; strings.TrimSpace(r.Body.String()) != want {
		t.Fatalf("%s", r.Body.String())
	}
	for _, s := range []string{importKeyA, importKeyB, "hdr-test-value"} {
		if strings.Contains(r.Body.String(), s) {
			t.Fatal("the answer carries a secret")
		}
	}
	list := h.storedLaunchers(ctx)
	if len(list) != 3 {
		t.Fatalf("%+v", list)
	}
	kimi, gcc := list[0], list[2]
	if kimi.Model != "kept" || kimi.Effort != "low" || kimi.KeySealed != sealed ||
		!reflect.DeepEqual(kimi.Models, []proto.ProviderModel{{ID: "kept"}, {ID: "kimi-k3", ContextWindow: 1000000}}) {
		t.Fatalf("an existing provider changed beyond its models: %+v", kimi)
	}
	if gcc.Name != "gcc-2" || gcc.Source != "zshrc:gcc@m1" || gcc.Auth != "" || gcc.APIKey != "" || gcc.Headers != "" || len(gcc.Models) != 2 {
		t.Fatalf("%+v", gcc)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), importKeyA) || strings.Contains(string(raw), "hdr-test-value") {
		t.Fatal("a secret stored in the clear")
	}
	if key, err := h.openKey(gcc.KeySealed); err != nil || key != importKeyA {
		t.Fatalf("key not sealed: %v", err)
	}
	if hd, err := h.openKey(gcc.HeadersSealed); err != nil || !strings.Contains(hd, "hdr-test-value") {
		t.Fatalf("headers not sealed: %v", err)
	}
	if r := importRequest(mux, "POST", `{"machine":"m1","providers":["nope"]}`); r.Code != 400 {
		t.Fatalf("an unknown provider: %d", r.Code)
	}
}

func TestLauncherImportMachineErrors(t *testing.T) {
	cases := []struct {
		err  error
		code int
		body string
	}{
		{fmt.Errorf("m1: %w", errOffline), 503, `"code":"offline"`},
		{&rpc.Error{Message: "unknown method launchers.zshrc"}, 409, `"code":"node_outdated"`},
		{errNoSuchMachine, 404, ""},
	}
	for _, c := range cases {
		_, mux := importTestHub(t, nil, c.err)
		for _, method := range []string{"GET", "POST"} {
			r := importRequest(mux, method, `{"machine":"m1","providers":["x"]}`)
			if r.Code != c.code || !strings.Contains(r.Body.String(), c.body) {
				t.Fatalf("%v %s: %d %s", c.err, method, r.Code, r.Body.String())
			}
		}
	}
}

func TestLauncherPickerDefault(t *testing.T) {
	h, _ := modelTestHub(t)
	ctx := context.Background()
	if err := h.saveModelRecord(ctx, "agent.new", 0, ModelBinding{Provider: "launcher.gw", Model: "m1", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Runtime: proto.AssistantRuntimeAgentSDK}); err != nil {
		t.Fatal(err)
	}
	if got := h.launcherDefault(ctx, "agent.new"); !reflect.DeepEqual(got, map[string]any{"provider": "gw", "model": "m1", "effort": "high"}) {
		t.Fatalf("agent.new: %+v", got)
	}
	if got := h.launcherDefault(ctx, "assistant"); got["runtime"] != "agentsdk" || got["provider"] != "claude" {
		t.Fatalf("assistant: %+v", got)
	}
	if got := h.launcherDefault(ctx, "kb"); got != nil {
		t.Fatalf("an API use has no launcher default: %+v", got)
	}
	r := httptest.NewRequest("GET", "/api/v1/machines/m1/launchers?purpose=assistant", nil)
	if got := h.pickerDefault(ctx, r); got["runtime"] != "agentsdk" {
		t.Fatalf("?purpose= ignored: %+v", got)
	}
}

// A proxy with a password is not imported: the proxy is stored and returned in the clear.
func TestImportDropsAProxyWithCredentials(t *testing.T) {
	a := zshrcAlias{Alias: "pcc", Command: "claude", Env: map[string]string{"HTTPS_PROXY": "http://u:pw-secret@127.0.0.1:7890"}}
	if g := newImportGroup("m1", a, "https://gw.example.com", "sk-test-x", "bearer", nil); g.Proxy != "" {
		t.Fatalf("proxy with credentials imported: %q", g.Proxy)
	}
	a.Env["HTTPS_PROXY"] = "http://127.0.0.1:7890"
	if g := newImportGroup("m1", a, "https://gw.example.com", "sk-test-x", "bearer", nil); g.Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("plain proxy dropped: %q", g.Proxy)
	}
}

// An alias named like a reserved launcher (claude itself, a /launchers/<word> route) is imported under another name.
func TestImportNameAvoidsReservedNames(t *testing.T) {
	for _, r := range reservedLauncherNames {
		if n := importName(r); slices.Contains(reservedLauncherNames, n) || !managedLauncherName.MatchString(n) {
			t.Errorf("%s imported as %q", r, n)
		}
	}
	if n := importName("glm"); n != "glm" {
		t.Errorf("glm imported as %q", n)
	}
}
