package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestParseZshrcAliases(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "zshrc", "aliases.zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	got := parseZshrcAliases(string(b))
	want := []zshrcAlias{
		{Alias: "gcc1", Command: "claude", Args: []string{"--dangerously-skip-permissions"}, Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://gw-one.example.com/api/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-test-one",
			"ANTHROPIC_MODEL": "glm-test-5", "ANTHROPIC_SMALL_FAST_MODEL": "glm-test-5-air"}},
		{Alias: "dcc2", Command: "claude", Args: []string{}, Env: map[string]string{
			"CLAUDE_CODE_SUBAGENT_MODEL": "deepseek-test-flash"}},
		{Alias: "mcc", Command: "claude", Args: []string{"--model", "minimax-test-m2", "--settings", "~/.claude/settings.mcc.json"},
			Env: map[string]string{"HTTPS_PROXY": "http://proxy.example.com:7890", "ANTHROPIC_BASE_URL": "https://gw-three.example.com",
				"ANTHROPIC_AUTH_TOKEN": "sk-test-three", "ANTHROPIC_CUSTOM_HEADERS": "X-Test-Team: alpha"}},
		{Alias: "ncc", Command: "claude", Args: []string{}, Env: map[string]string{}},
		// Defined twice: the last one, where it is.
		{Alias: "kcc", Command: "claude", Args: []string{}, Env: map[string]string{
			"ANTHROPIC_BASE_URL": "https://gw-two.example.com/anthropic", "ANTHROPIC_API_KEY": "sk-test-two-new",
			"ANTHROPIC_MODEL": "kimi-test-k2[1m]"}},
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("got %s", gj)
	}
}

func TestZshrcLaunchersRPC(t *testing.T) {
	n, _ := testNode(t)
	dir := t.TempDir()
	t.Setenv("ZDOTDIR", dir)
	call := func() []zshrcAlias {
		t.Helper()
		res, err := n.handle(context.Background(), "launchers.zshrc", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res)
		var out struct {
			Aliases []zshrcAlias `json:"aliases"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Aliases == nil {
			t.Fatalf("%s: %v", raw, err)
		}
		return out.Aliases
	}
	if got := call(); len(got) != 0 { // no ~/.zshrc: none, not an error
		t.Fatalf("got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(`alias tcc='ANTHROPIC_API_KEY=sk-test-rpc claude -c'`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := call()
	if len(got) != 1 || got[0].Alias != "tcc" || got[0].Env["ANTHROPIC_API_KEY"] != "sk-test-rpc" || !slices.Equal(got[0].Args, []string{"-c"}) {
		t.Fatalf("got %+v", got)
	}
}
