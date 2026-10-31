package node

import (
	"slices"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
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
	for _, want := range []string{"ANTHROPIC_BASE_URL=http://gw.example.com", "ANTHROPIC_API_KEY=k", "ANTHROPIC_MODEL=m1",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=m1", "ANTHROPIC_SMALL_FAST_MODEL=m1"} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s: %v", want, env)
		}
	}
	if e := n.launcherEnv("nope"); e["error"] == nil {
		t.Fatal("an unknown name is an error")
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
