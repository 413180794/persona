package assistant

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// 设置 › 模型供应商 with a bash machine, an offline one and one too slow to answer (#2854): the bash machine's
// launchers come with their rc file, plain claude shows as the built-in launcher by machine, every machine says how
// its list came in, and the page never waits past the per-machine timeout.
func TestLauncherListSaysHowEachMachineSynced(t *testing.T) {
	s, f, _ := modelService(t)
	old := launcherSyncTimeout
	launcherSyncTimeout = 300 * time.Millisecond
	t.Cleanup(func() { launcherSyncTimeout = old })
	f.mu.Lock()
	f.devices = []Device{
		{ID: "box", Name: "Box", Online: true, Reachable: true, ClaudeVersion: "2.1.0"},
		{ID: "slow", Name: "Slow", Online: true, Reachable: true},
		{ID: "away", Name: "Away"},
	}
	f.zshrc = map[string][]proto.Launcher{"box": {{Name: "claude", Model: "默认"}, {Name: "codex", Model: "默认", Agent: "codex", Source: "zshrc"},
		{Name: "kcc", BaseURL: "https://gw.example.com", Source: "zshrc"}}}
	f.rcFiles = map[string]string{"box": "~/.bashrc"}
	f.slowLaunchers = map[string]bool{"slow": true}
	for _, id := range []string{"box", "slow", "away"} {
		f.sessions[id] = nil
	}
	f.mu.Unlock()

	started := time.Now()
	code, raw := launcherCall(t, s, "GET", "/api/v1/launchers", "")
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("the list waited %v for the slow machine", took)
	}
	var res struct {
		Launchers []LauncherView `json:"launchers"`
		Sync      []LauncherSync `json:"sync"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(raw), &res) != nil {
		t.Fatalf("%d %s", code, raw)
	}
	byName := map[string]LauncherView{}
	for _, v := range res.Launchers {
		byName[v.Name] = v
	}
	if v := byName["kcc"]; v.Source != "zshrc" || !slices.Equal(v.RCFiles, []string{"~/.bashrc"}) || !slices.Equal(v.Machines, []string{"box"}) || len(v.Stale) != 0 {
		t.Errorf("kcc: %+v", v)
	}
	if v := byName["claude"]; v.Source != "builtin" || !slices.Equal(v.Machines, []string{"box"}) || v.Versions["box"] != "2.1.0" {
		t.Errorf("claude: %+v", v)
	}
	states := map[string]LauncherSync{}
	for _, st := range res.Sync {
		states[st.Machine] = st
	}
	if st := states["box"]; st.State != "ok" || st.RCFile != "~/.bashrc" || st.Shell != "bash" || st.SyncedAt == nil || st.Name != "Box" {
		t.Errorf("box: %+v", st)
	}
	if st := states["slow"]; st.State != "timeout" {
		t.Errorf("slow: %+v", st)
	}
	if st := states["away"]; st.State != "offline" {
		t.Errorf("away: %+v", st)
	}

	// The built-in claude has its own page and cannot be hidden (that would take plain claude out of every picker).
	if code, raw := launcherCall(t, s, "GET", "/api/v1/launchers/claude", ""); code != http.StatusOK {
		t.Errorf("detail: %d %s", code, raw)
	}
	if code, raw := launcherCall(t, s, "DELETE", "/api/v1/launchers/claude", ""); code != http.StatusBadRequest {
		t.Errorf("hide claude: %d %s", code, raw)
	}
	// 模型配置's 「Claude Code 默认」 says where it is.
	for _, p := range s.modelProviders(t.Context()) {
		if p.ID == "launcher.claude" && !slices.Equal(p.Machines, []string{"box"}) {
			t.Errorf("launcher.claude machines %v", p.Machines)
		}
	}
}

// A machine that answered before and is offline now keeps its launchers, marked stale, with its rc file.
func TestOfflineMachinesLaunchersAreStale(t *testing.T) {
	s, f, _ := modelService(t)
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away", Online: true, Reachable: true}}
	f.zshrc = map[string][]proto.Launcher{"away": {{Name: "kcc", BaseURL: "https://gw.example.com", Source: "zshrc"}}}
	f.sessions["away"] = nil
	f.mu.Unlock()
	if code, raw := launcherCall(t, s, "GET", "/api/v1/launchers", ""); code != 200 {
		t.Fatal(code, raw)
	}
	f.mu.Lock()
	f.devices = []Device{{ID: "away", Name: "Away"}}
	f.mu.Unlock()
	_, raw := launcherCall(t, s, "GET", "/api/v1/launchers", "")
	var res struct {
		Launchers []LauncherView `json:"launchers"`
		Sync      []LauncherSync `json:"sync"`
	}
	_ = json.Unmarshal([]byte(raw), &res)
	i := slices.IndexFunc(res.Launchers, func(v LauncherView) bool { return v.Name == "kcc" })
	if i < 0 || !slices.Equal(res.Launchers[i].Stale, []string{"away"}) || !slices.Equal(res.Launchers[i].RCFiles, []string{"~/.zshrc"}) {
		t.Fatalf("%s", raw)
	}
	if len(res.Sync) != 1 || res.Sync[0].State != "offline" || res.Sync[0].SyncedAt == nil {
		t.Fatalf("sync: %+v", res.Sync)
	}
}
