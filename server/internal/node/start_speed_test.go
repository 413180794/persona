package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// seedLauncherModels puts list as launcher name's kept Codex model list on n, as a launchers.models ask would.
func seedLauncherModels(t *testing.T, n *Node, name string, models ...proto.AgentModel) {
	t.Helper()
	key := n.home + "\x00" + name + "\x00codex\x00" + n.executionFingerprint(name, modelExecution{})
	launcherModelsCache.Lock()
	launcherModelsCache.at[key], launcherModelsCache.got[key] = time.Now(), proto.LauncherModels{Agent: "codex", Models: models}
	launcherModelsCache.Unlock()
	t.Cleanup(func() {
		launcherModelsCache.Lock()
		delete(launcherModelsCache.at, key)
		delete(launcherModelsCache.got, key)
		launcherModelsCache.Unlock()
	})
}

// A Codex start at a faster speed runs its native tier, `-c service_tier=…` beside the effort, when the model's catalog
// offers it; a model that does not is refused before any tab opens; one the catalog does not list is Codex's to run.
func TestCodexStartSpeed(t *testing.T) {
	fast := proto.ServiceTier{ID: "priority", Name: "Fast"}
	ultra := proto.ServiceTier{ID: "ultrafast", Name: "Ultrafast"}
	for _, tc := range []struct {
		name, model, speed string
		want               []string // the argv; nil: refused
	}{
		{"default model fast", "", "fast", []string{"codex", "-c", "service_tier=priority"}},
		{"ultrafast", "gpt-sol", "ultrafast", []string{"codex", "--model", "gpt-sol", "-c", "service_tier=ultrafast"}},
		{"standard", "", "standard", []string{"codex"}},
		{"not offered", "", "ultrafast", nil},
		{"no tiers", "gpt-plain", "fast", nil},
		{"not listed", "gpt-custom", "fast", []string{"codex", "--model", "gpt-custom", "-c", "service_tier=priority"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCodexLaunchers(t, map[string][]string{"cx": {"codex"}})
			n, f, _, _ := codexStartHerdr(t, 0)
			seedLauncherModels(t, n, "cx",
				proto.AgentModel{ID: "gpt-astra", Default: true, ServiceTiers: []proto.ServiceTier{fast}},
				proto.AgentModel{ID: "gpt-sol", ServiceTiers: []proto.ServiceTier{fast, ultra}},
				proto.AgentModel{ID: "gpt-plain"})
			params := map[string]any{"cwd": "~/proj", "launcher": "cx", "prompt": "hello", "speed": tc.speed}
			if tc.model != "" {
				params["model"] = tc.model
			}
			_, err := request(t, n, "session.start", params)
			if tc.want == nil {
				if refusalCode(err) != "agent_speed_model" || !strings.Contains(err.Error(), tc.speed) {
					t.Fatalf("error = %v (%s), want agent_speed_model naming the speed", err, refusalCode(err))
				}
				if got := f.trace("tab.create"); len(got) != 0 {
					t.Fatalf("a refused start opened a tab: %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ran, tc.want) {
				t.Fatalf("runs %q, want %q", ran, tc.want)
			}
		})
	}
}

// A Claude Code start at fast turns fast mode on through the --settings file it starts with: a copy of Persona's
// settings with "fastMode": true. The async start (the app's, a task's Claude) carries it too.
func TestClaudeStartFastMode(t *testing.T) {
	withClaudeVersion(t, "2.1.318")
	n, f, home := herdrNode(t)
	if err := n.writeClaudeConfig(); err != nil {
		t.Fatal(err)
	}
	cwd := mkdir(t, filepath.Join(home, "proj"))
	f.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "proj"}}})
	startHerdr(f, "w1:p2", proto.StatusIdle)
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	f.screenSteps("w1:p2", isKeys(), screenFixture(t, "claude-2.1.312-empty-162.txt"))
	if _, err := request(t, n, "session.start", map[string]any{"session_id": startID, "cwd": cwd, "launcher": "claude", "async": true, "speed": "fast"}); err != nil {
		t.Fatal(err)
	}
	waitCalls(t, f, "pane.send_input", 1)
	settings := regexp.MustCompile(` --settings '?([^ ']*claude-fast/[0-9a-f]+\.json)'?`)
	var path string
	for _, c := range f.trace("pane.send_input") {
		if m := settings.FindStringSubmatch(c); m != nil && strings.Contains(c, "--session-id "+startID) {
			path = m[1]
		}
	}
	if path == "" {
		t.Fatalf("no fast-mode --settings typed: %v", f.trace("pane.send_input"))
	}
	var values map[string]any
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &values) != nil {
		t.Fatalf("settings %s: %v", path, err)
	}
	if values["fastMode"] != true {
		t.Fatalf("fast settings = %s", b)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("fast settings mode = %v, %v", fi, err)
	}
}

// claudeFastSettings copies the settings it is given (hooks, a gateway's env) and adds fast mode; the same settings
// give the same file, and none give one with only fast mode.
func TestClaudeFastSettings(t *testing.T) {
	n := &Node{dir: t.TempDir()}
	base := filepath.Join(n.dir, "claude-settings.json")
	if err := os.WriteFile(base, []byte(`{"hooks":{"Stop":[]},"env":{"A":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := n.claudeFastSettings(base)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &values) != nil || values["fastMode"] != true || values["hooks"] == nil || values["env"] == nil {
		t.Fatalf("fast settings = %s", b)
	}
	if again, err := n.claudeFastSettings(base); err != nil || again != path {
		t.Fatalf("again = %q, %v; want %q", again, err, path)
	}
	none, err := n.claudeFastSettings("")
	if err != nil || none == path {
		t.Fatalf("no settings = %q, %v", none, err)
	}
	if missing, err := n.claudeFastSettings(filepath.Join(n.dir, "gone.json")); err != nil || missing != none {
		t.Fatalf("missing settings = %q, %v; want %q", missing, err, none)
	}
}

// A faster speed is refused before anything starts wherever the start would not run it: another agent, Claude Code
// at ultrafast, a resume, an unknown speed. Nothing reaches herdr.
func TestStartSpeedRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
		code   string
	}{
		{"agy", map[string]any{"launcher": "agy", "speed": "fast"}, "agent_speed_unsupported"},
		{"pi", map[string]any{"launcher": "pi", "speed": "fast"}, "agent_speed_unsupported"},
		{"traex", map[string]any{"launcher": "traex", "speed": "fast"}, "agent_speed_unsupported"},
		{"claude ultrafast", map[string]any{"launcher": "claude", "speed": "ultrafast"}, "agent_speed_unsupported"},
		{"declared agent", map[string]any{"launcher": "work_cli", "agent_type": "cursor", "speed": "fast"}, "agent_speed_unsupported"},
		{"resume", map[string]any{"launcher": "claude", "speed": "fast", "resume_session_id": startID}, "agent_speed_start_only"},
		{"unknown", map[string]any{"launcher": "claude", "speed": "warp"}, "agent_speed_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, home := herdrNode(t)
			tc.params["cwd"], tc.params["prompt"] = mkdir(t, filepath.Join(home, "proj")), "hello"
			_, err := request(t, n, "session.start", tc.params)
			if refusalCode(err) != tc.code {
				t.Fatalf("error = %v (%s), want %s", err, refusalCode(err), tc.code)
			}
			if !strings.Contains(err.Error(), "No agent was started") && !strings.Contains(err.Error(), "没有启动") {
				t.Fatalf("refusal does not say nothing started: %v", err)
			}
			if got := f.trace("tab.create"); len(got) != 0 {
				t.Fatalf("a refused start opened a tab: %v", got)
			}
		})
	}
}
