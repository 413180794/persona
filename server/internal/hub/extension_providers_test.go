package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestPresetForPrefersTheAddedFromPresetThenTheGateway(t *testing.T) {
	presets := []LauncherPreset{
		{ID: "gw/api", Kind: "api", BaseURL: "https://gw.example.com"},
		{ID: "gw/team", Kind: "launcher", BaseURL: "https://gw.example.com/"},
		{ID: "gw/relay", Kind: "launcher", BaseURL: "https://relay.example.com"},
	}
	if pr, ok := presetFor(presets, "gw/relay", "https://gw.example.com"); !ok || pr.ID != "gw/relay" {
		t.Fatal("the preset it was added from wins:", pr.ID, ok)
	}
	if pr, ok := presetFor(presets, "", "HTTPS://GW.example.com"); !ok || pr.ID != "gw/team" {
		t.Fatal("by gateway, launcher kind only, case and trailing slash ignored:", pr.ID, ok)
	}
	if _, ok := presetFor(presets, "", "https://other.example.com"); ok {
		t.Fatal("no preset for another gateway")
	}
	if _, ok := presetFor(presets, "", ""); ok {
		t.Fatal("no gateway matches nothing")
	}
}

func TestWithPresetModelsKeepsTheLaunchersOwnFirst(t *testing.T) {
	res := proto.LauncherModels{Agent: "claude", Models: []proto.AgentModel{
		{ID: "default", Default: true}, {ID: "opus", Resolved: "m-1"}, {ID: "m-2"},
	}}
	pr := LauncherPreset{Name: "Example Gateway", Models: []extension.ProviderModel{
		{ID: "m-1"}, {ID: "m-2"}, {ID: "m-3", Name: "Model Three"}, {ID: ""}, {ID: "m-4"}, {ID: "m-3"},
	}}
	got := withPresetModels(res, pr).Models
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	want := []string{"default", "opus", "m-2", "m-3", "m-4"}
	if len(ids) != len(want) {
		t.Fatal(ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatal(ids)
		}
	}
	if got[3].Name != "Model Three" || got[3].Description != "Example Gateway" || got[4].Name != "m-4" {
		t.Fatalf("%+v", got[3:])
	}
}

// 新建 Agent's model picker offers the plugin gateway's models for a ~/.zshrc launcher on that gateway and for a
// provider added from the preset, after the launcher's own /model entries; another gateway's launcher is left alone.
func TestLauncherModelsIncludeThePresetsModels(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(`{"manifest_version": 1, "id": "example-gw", "name": "Example 网关",
		"version": "1.0.0", "contributes": {"providers": [{"id": "team", "name": "Example Team", "kind": "launcher",
		"base_url": "https://gw.example.com", "model": "gw-large", "models": [{"id": "gw-large", "name": "Large"}, {"id": "gw-small"}]}]}}`), 0o644)
	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+dir+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/launchers", master, `{"name": "teamgw", "preset": "example-gw/team", "api_key": "sk-x"}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	modelsNode(t, h, MachineConfig{ID: "box"}, func(method string, p map[string]any) (any, error) {
		switch method {
		case "launchers.list":
			return map[string]any{"launchers": []proto.Launcher{
				{Name: "gw-alias", BaseURL: "https://gw.example.com/"},
				{Name: "elsewhere", BaseURL: "https://other.example.com"},
			}}, nil
		case "launchers.models":
			return proto.LauncherModels{Agent: "claude", Models: []proto.AgentModel{{ID: "default", Default: true}, {ID: "gw-large"}}}, nil
		}
		return map[string]any{}, nil
	})
	ids := func(name string) []string {
		w := modelsRequest(h, "GET", "/api/v1/machines/box/launchers/"+name+"/models", "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
		var res proto.LauncherModels
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		var out []string
		for _, m := range res.Models {
			out = append(out, m.ID)
		}
		return out
	}
	for _, name := range []string{"gw-alias", "teamgw"} {
		if got := fmt.Sprint(ids(name)); got != "[default gw-large gw-small]" {
			t.Fatalf("%s: %s", name, got)
		}
	}
	if got := fmt.Sprint(ids("elsewhere")); got != "[default gw-large]" {
		t.Fatalf("another gateway's launcher got the preset's models: %s", got)
	}
	m := h.byID["box"]
	if !h.launcherHasModel(t.Context(), m, "gw-alias", "gw-small") {
		t.Fatal("a send with a preset model is checked against the list that offered it")
	}

	// A machine slow to list its launchers (tailcat right after a restart): the gateway it was last seen with is used,
	// not a second node call that would time the picker out.
	modelsNode(t, h, MachineConfig{ID: "slow"}, func(method string, p map[string]any) (any, error) {
		if method == "launchers.list" {
			return nil, fmt.Errorf("not now")
		}
		if method == "launchers.models" {
			return proto.LauncherModels{Agent: "claude", Models: []proto.AgentModel{{ID: "default", Default: true}}}, nil
		}
		return map[string]any{}, nil
	})
	h.launchers.mu.Lock()
	h.launchersSeen = map[string][]proto.Launcher{"slow": {{Name: "gw-alias", BaseURL: "https://gw.example.com"}}}
	h.launchers.mu.Unlock()
	w := modelsRequest(h, "GET", "/api/v1/machines/slow/launchers/gw-alias/models", "")
	var res proto.LauncherModels
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Models) != 3 {
		t.Fatalf("the last seen gateway is not used: %d %s", w.Code, w.Body)
	}
}
