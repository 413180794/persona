package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantCompactionLimits(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		main                   bool
		capacity, window, idle int
	}{
		{"main", true, 1_000_000, 400_000, 300_000},
		{"ordinary", false, 1_000_000, 200_000, 150_000},
		{"legacy ordinary", false, 0, 200_000, 150_000},
		{"main 200K backend", true, 200_000, 200_000, 150_000},
		{"main 300K backend", true, 300_000, 300_000, 225_000},
		{"main 128K backend", true, 128_000, 128_000, 96_000},
		{"ordinary 128K backend", false, 128_000, 128_000, 96_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := compactionLimits(tt.main, tt.capacity)
			if c.window != tt.window || c.idle != tt.idle {
				t.Fatalf("limits: %+v, want %d/%d", c, tt.window, tt.idle)
			}
			p := &assistantProc{agent: "claude", compactIdleAt: c.idle, context: c.idle - 1}
			if p.idleCompactable() {
				t.Fatal("compacts below idle threshold")
			}
			p.context++
			if !p.idleCompactable() {
				t.Fatal("does not compact at idle threshold")
			}
			for _, args := range [][]string{c.env(), codexArgs("personad", "test", c.window)} {
				if !strings.Contains(strings.Join(args, "\n"), "="+strconv.Itoa(tt.window)) {
					t.Fatalf("runtime window not set: %q", args)
				}
			}
		})
	}
}

func TestAssistantProviderCapacity(t *testing.T) {
	base := proto.ManagedLauncher{Name: "gateway", Model: "big", Models: []proto.ProviderModel{
		{ID: "big", ContextWindow: 1_000_000}, {ID: "small", ContextWindow: 128_000},
	}, CompactWindow: 200_000}
	for _, tt := range []struct {
		name, model, env string
		max, want        int
	}{
		{"real capacity not compact policy", "big", "", 0, 1_000_000},
		{"selected small model", "small", "", 0, 128_000},
		{"explicit max", "big", "", 200_000, 200_000},
		{"model beats inflated max", "small", "", 1_000_000, 128_000},
		{"env max", "big", "CLAUDE_CODE_MAX_CONTEXT_TOKENS=128000", 0, 128_000},
		{"auto env does not mean capacity", "big", "CLAUDE_CODE_AUTO_COMPACT_WINDOW=150000", 0, 1_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := base
			l.MaxContext, l.Env = tt.max, tt.env
			if got := providerCapacity(l, tt.model); got != tt.want {
				t.Fatalf("capacity %d, want %d", got, tt.want)
			}
			if got := providerEnv(l, tt.model, "", true)["CLAUDE_CODE_MAX_CONTEXT_TOKENS"]; got != strconv.Itoa(tt.want) {
				t.Fatalf("MAX_CONTEXT_TOKENS %s, want %d", got, tt.want)
			}
		})
	}
}

func TestAssistantCompactionRPCAndSpare(t *testing.T) {
	for _, main := range []bool{true, false} {
		t.Run(strconv.FormatBool(main), func(t *testing.T) {
			fakeClaudeHome(t, "read line\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
			t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "1000000")
			n := codexTestNode(t)
			n.dir = t.TempDir()
			withFakeHub(t, n)
			params := map[string]any{"launcher": "claude", "cwd": t.TempDir(), "text": "hello"}
			if main {
				params["is_main"] = true
			} else {
				params["session_id"] = "11111111-2222-3333-4444-555555555555"
				n.state.put(params["session_id"].(string), sessionState{IsMain: true})
			}
			raw, err := nodeCall(t, n, "assistant.send", params)
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			defer n.stopAssistant(out.SessionID)
			p := n.procs[out.SessionID]
			c := compactionLimits(main, 1_000_000)
			if p.compactIdleAt != c.idle || envValue(p.cmd.Env, "CLAUDE_CODE_AUTO_COMPACT_WINDOW") != strconv.Itoa(c.window) {
				t.Fatalf("process limits: idle %d, env %v", p.compactIdleAt, p.cmd.Env)
			}
			st, _ := n.state.get(out.SessionID)
			if st.IsMain != main {
				t.Fatalf("stored scope %v", st.IsMain)
			}
			n.spare = p
			if main && n.adoptSpare(p.cwd, p.launcher, modelExecution{}) != nil {
				t.Fatal("a main process was adopted for an ordinary conversation")
			}
		})
	}
}

func TestAssistantCompactionSettingsAndSDK(t *testing.T) {
	n := assistantNode(t)
	n.dir = t.TempDir()
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"hooks":{"Stop":[]},"env":{"KEEP":"yes","CLAUDE_CODE_AUTO_COMPACT_WINDOW":"200000","CLAUDE_CODE_MAX_CONTEXT_TOKENS":"200000"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compactionLimits(true, 1_000_000)
	c.ownSettings = `{"env":{"LAUNCHER":"yes"},"permissions":{"allow":["Read"]}}`
	path, err := n.assistantCompactionSettings("test-id", n.home, settings, c)
	if err != nil {
		t.Fatal(err)
	}
	s, err := readLauncherSettings(path, n.home, n.home)
	if err != nil {
		t.Fatal(err)
	}
	env := s["env"].(map[string]any)
	if env["KEEP"] != "yes" || env["LAUNCHER"] != "yes" || env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" || s["hooks"] == nil || s["permissions"] == nil {
		t.Fatalf("merged settings %v", s)
	}
	start := sdkStart{settings: path, mcp: map[string]any{}, compaction: &c, env: map[string]string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "200000", "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "200000"}}
	opts, err := start.options("claude")
	if err != nil {
		t.Fatal(err)
	}
	env = opts["settings"].(map[string]any)["env"].(map[string]any)
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" {
		t.Fatalf("SDK clamped policy back down: %v", env)
	}
	if mode, err := os.Stat(path); err != nil || mode.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions: %v %v", mode, err)
	}
}

func TestAssistantCodexCapacity(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want int
	}{
		{[]string{"-c", "model_context_window=128000"}, 128000},
		{[]string{"--config=model_context_window=200_000"}, 200000},
		{[]string{"model_context_window=1000000\n[profiles.small]\nmodel_context_window=128000 # cap\n"}, 128000},
		{[]string{"-c", "model_auto_compact_token_limit=200000"}, 0},
	} {
		if got := codexCapacity(tt.args); got != tt.want {
			t.Fatalf("%q: %d, want %d", tt.args, got, tt.want)
		}
	}
}

func TestAssistantShellLauncherCompaction(t *testing.T) {
	for _, tt := range []struct {
		name, rc     string
		window, idle int
	}{
		{"large backend", `function testproxy { export CLAUDE_CODE_MAX_CONTEXT_TOKENS=1000000 CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000; command claude "$@"; }`, 400000, 300000},
		{"small backend", `function testproxy { export CLAUDE_CODE_MAX_CONTEXT_TOKENS=128000 CLAUDE_CODE_AUTO_COMPACT_WINDOW=128000; command claude "$@"; }`, 128000, 96000},
		{"unknown backend", `function testproxy { export CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000; command claude "$@"; }`, 200000, 150000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaudeHome(t, "read line\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
			t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
			home := os.Getenv("HOME")
			if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(tt.rc+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			n := codexTestNode(t)
			n.dir = t.TempDir()
			withFakeHub(t, n)
			p, err := n.spawnClaude("11111111-2222-3333-4444-555555555555", t.TempDir(), "testproxy", false, false, modelExecution{IsMain: true})
			if err != nil {
				t.Fatal(err)
			}
			defer n.stopAssistant(p.id)
			settings, err := readLauncherSettings(settingsArg(p.cmd.Args), p.cwd, home)
			if err != nil {
				entries, _ := os.ReadDir(filepath.Join(n.dir, "launcher-settings"))
				t.Fatalf("%v; args=%q; settings files=%v", err, p.cmd.Args, entries)
			}
			env := settings["env"].(map[string]any)
			if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != strconv.Itoa(tt.window) || p.compactIdleAt != tt.idle {
				t.Fatalf("settings %v, idle %d", env, p.compactIdleAt)
			}
		})
	}
}

func TestAssistantSDKCompactionStart(t *testing.T) {
	f := sdkTest(t)
	f.n.setManaged([]proto.ManagedLauncher{{Name: "gw", Model: "big", CompactWindow: 200000, Models: []proto.ProviderModel{{ID: "big", ContextWindow: 1000000}}}})
	execution := sdkExec(proto.AssistantRuntimeAgentSDK)
	execution.IsMain = true
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "gw", "hello", nil, "", "", execution)
	if err != nil {
		t.Fatal(err)
	}
	opts := f.start(t)
	env := opts["settings"].(map[string]any)["env"].(map[string]any)
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "400000" || env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1000000" || f.proc(id).compactIdleAt != 300000 {
		t.Fatalf("SDK start: %v, idle %d", env, f.proc(id).compactIdleAt)
	}
}
