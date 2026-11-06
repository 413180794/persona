package node

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestManagedLauncherRunsThroughJarvisd(t *testing.T) {
	n := &Node{bin: "/opt/jarvis bin/jarvisd"}
	if got := n.launcherCommand("cc_kiro"); got != "cc_kiro" {
		t.Fatalf("a ~/.zshrc launcher is its own name: %q", got)
	}
	n.setManaged([]proto.ManagedLauncher{{Name: "jv_strong", BaseURL: "http://gw.example.com", Model: "m1", APIKey: "k"}})
	if got := n.launcherCommand("jv_strong"); got != "'/opt/jarvis bin/jarvisd' launch jv_strong" {
		t.Fatalf("a managed one runs as jarvisd launch: %q", got)
	}
	env := n.launcherEnv("jv_strong")["env"].([]string)
	// ANTHROPIC_AUTH_TOKEN is there and empty: Claude Code would otherwise take one from ~/.claude/settings.json and
	// send it as a second credential.
	for _, want := range []string{"ANTHROPIC_BASE_URL=http://gw.example.com", "ANTHROPIC_API_KEY=k", "ANTHROPIC_AUTH_TOKEN=",
		"ANTHROPIC_MODEL=m1", "ANTHROPIC_DEFAULT_OPUS_MODEL=m1", "ANTHROPIC_SMALL_FAST_MODEL=m1"} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s: %v", want, env)
		}
	}
	if e := n.launcherEnv("nope"); e["error"] == nil {
		t.Fatal("an unknown name is an error")
	}
}

// Subagents follow the model unless a subagent model is set; a custom model entry is passed with its labels.
func TestManagedEnvSubagentAndCustomModel(t *testing.T) {
	base := proto.ManagedLauncher{Name: "l", BaseURL: "http://gw", Model: "m1"}
	if env := managedEnv(base); !slices.Contains(env, "CLAUDE_CODE_SUBAGENT_MODEL=m1") {
		t.Fatalf("subagents do not follow the model: %v", env)
	}
	// A gateway cannot carry Claude Code's Artifacts: off for every managed start (artifactsOff).
	if env := managedEnv(base); !slices.Contains(env, "CLAUDE_CODE_DISABLE_ARTIFACT=1") {
		t.Fatalf("Artifacts are not turned off: %v", env)
	}
	base.SubagentModel, base.CustomModel = "m2", "m3"
	base.CustomModelName, base.CustomModelDescription = "Third", "a model"
	env := managedEnv(base)
	for _, want := range []string{"CLAUDE_CODE_SUBAGENT_MODEL=m2", "ANTHROPIC_CUSTOM_MODEL_OPTION=m3",
		"ANTHROPIC_CUSTOM_MODEL_OPTION_NAME=Third", "ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION=a model"} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s: %v", want, env)
		}
	}
}

// A node started by launchd has a minimal PATH: a claude installed outside it must still be found.
func TestClaudeExecutableFallsBackToInstallLocations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	want := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := claudeExecutable()
	if err != nil || got != want {
		t.Fatalf("got %q %v, want %q", got, err, want)
	}
}

func TestManagedLauncherBearerAuth(t *testing.T) {
	n := &Node{bin: "jarvisd"}
	n.setManaged([]proto.ManagedLauncher{{Name: "gw", BaseURL: "http://gw.example.com", Model: "m1", APIKey: "k", Auth: "bearer"}})
	env := n.launcherEnv("gw")["env"].([]string)
	if !slices.Contains(env, "ANTHROPIC_AUTH_TOKEN=k") || !slices.Contains(env, "ANTHROPIC_API_KEY=") {
		t.Fatalf("a bearer launcher's key goes in ANTHROPIC_AUTH_TOKEN, and ANTHROPIC_API_KEY is empty: %v", env)
	}
	if err := approveClaudeAPIKey(append(env, "CLAUDE_CONFIG_DIR="+t.TempDir())); err != nil {
		t.Fatalf("no API key, nothing to approve: %v", err)
	}
	// The --settings env (launcher_settings.go) carries the same: the key, and the other variable empty, so the user's
	// settings.json cannot fill it in.
	for _, l := range []proto.ManagedLauncher{
		{BaseURL: "http://gw.example.com", APIKey: "k", Auth: "bearer"},
		{BaseURL: "http://gw.example.com", APIKey: "k"},
	} {
		got := managedSettingsEnv(l)
		key, other := "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"
		if l.BearerAuth() {
			key, other = other, key
		}
		if v, ok := got[other]; got[key] != "k" || !ok || v != "" {
			t.Errorf("auth %q: settings env %v", l.Auth, got)
		}
	}
}

// Without a key (none stored, or the hub could not open it) no credential variable is set, not even an empty one:
// Claude keeps what its settings or sign-in give it.
func TestManagedLauncherWithoutKeySetsNoCredential(t *testing.T) {
	for _, auth := range []string{"", "bearer"} {
		env := managedEnv(proto.ManagedLauncher{Name: "gw", BaseURL: "http://gw.example.com", Model: "m1", Auth: auth})
		for _, kv := range env {
			if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") || strings.HasPrefix(kv, "ANTHROPIC_AUTH_TOKEN=") {
				t.Errorf("auth %q: %s set without a key: %v", auth, kv, env)
			}
		}
		if _, ok := managedSettingsEnv(proto.ManagedLauncher{BaseURL: "http://gw.example.com", Auth: auth})["ANTHROPIC_AUTH_TOKEN"]; ok {
			t.Errorf("auth %q: the --settings env blanks a credential without a key", auth)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	def := strings.Join([]string{
		`export ANTHROPIC_API_KEY="sk-live-123"`,
		`export ANTHROPIC_AUTH_TOKEN=abc`,
		`export ANTHROPIC_API_KEY="$(<"$key_file")"`,
		`export ANTHROPIC_MODEL="m1"`,
	}, "\n")
	got := redactSecrets(def)
	if strings.Contains(got, "sk-live-123") || strings.Contains(got, "=abc") {
		t.Fatalf("a literal secret shows: %s", got)
	}
	if !strings.Contains(got, `$(<"$key_file")`) || !strings.Contains(got, `ANTHROPIC_MODEL="m1"`) {
		t.Fatalf("where a key comes from and other settings stay: %s", got)
	}
}

// Plain claude starts in the machine's claude_start_dir (node.configure) when one is set: a computer logged in under
// a profile directory, not ~/.claude, answers "Not logged in" otherwise (#1459). Other launchers keep their own, a
// call that says nothing of it leaves it, and "" clears it.
func TestPlainClaudeStartsInTheConfiguredDir(t *testing.T) {
	n, _ := testNode(t)
	n.bin = "/opt/jarvisd"
	n.setManaged([]proto.ManagedLauncher{{Name: "jv_strong", BaseURL: "http://gw.example.com"}})
	if got := n.launcherCommand("claude"); got != "claude" {
		t.Fatalf("unset: %q", got)
	}
	for _, c := range []struct{ dir, want string }{
		{"~/.config/claude-code/profiles/work", `CLAUDE_CONFIG_DIR="$HOME"/.config/claude-code/profiles/work claude`},
		{"/Users/me/My Claude", `CLAUDE_CONFIG_DIR='/Users/me/My Claude' claude`},
		{"~", `CLAUDE_CONFIG_DIR="$HOME" claude`},
	} {
		configureNode(t, n, map[string]any{"claude_start_dir": c.dir})
		if got := n.launcherCommand("claude"); got != c.want {
			t.Errorf("%s: %q, want %q", c.dir, got, c.want)
		}
	}
	if got := n.launcherCommand("cc_kiro"); got != "cc_kiro" {
		t.Errorf("a ~/.zshrc launcher keeps its own: %q", got)
	}
	if got := n.launcherCommand("jv_strong"); got != "/opt/jarvisd launch jv_strong" {
		t.Errorf("a managed launcher keeps its own: %q", got)
	}
	configureNode(t, n, map[string]any{"botmux": true})
	if got := n.launcherCommand("claude"); got != `CLAUDE_CONFIG_DIR="$HOME" claude` {
		t.Errorf("a call without it changed it: %q", got)
	}
	configureNode(t, n, map[string]any{"claude_start_dir": ""})
	if got := n.launcherCommand("claude"); got != "claude" {
		t.Errorf("cleared: %q", got)
	}
}
