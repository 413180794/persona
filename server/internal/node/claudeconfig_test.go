package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSettingsKeepOtherKeys(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	n := &Node{home: home}
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	path := filepath.Join(home, ".claude", "settings.json")
	_ = os.WriteFile(path, []byte(`{"hooks":{"Stop":[]},"language":"中文"}`), 0o600)

	if err := n.setClaudeSetting("awaySummaryEnabled", false); err != nil {
		t.Fatal(err)
	}
	if err := n.setClaudeSetting("effortLevel", "nope"); err == nil {
		t.Fatal("invalid enum accepted")
	}
	if err := n.setClaudeSetting("hooks", map[string]any{}); err == nil {
		t.Fatal("non-editable key accepted")
	}
	if err := n.setClaudeSetting("language", nil); err != nil {
		t.Fatal(err)
	}
	if err := n.setClaudeSetting("effortLevel", "xhigh"); err != nil {
		t.Fatal(err)
	}
	if err := n.setClaudeSetting("effortLevel", "auto"); err != nil { // auto is "not set": Claude Code ignores the value
		t.Fatal(err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["effortLevel"]; ok {
		t.Fatalf("auto written as %v", raw["effortLevel"])
	}
	if raw["awaySummaryEnabled"] != false || raw["hooks"] == nil || raw["language"] != nil {
		t.Fatalf("settings = %v", raw)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %o", st.Mode().Perm())
	}
	list, err := n.getClaudeSettings()
	if err != nil || list[0].Key != "awaySummaryEnabled" || list[0].Value != false {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestClaudeWorkflowsSetting(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	n := &Node{home: t.TempDir()}
	read := func() ClaudeSetting {
		t.Helper()
		list, err := n.getClaudeSettings()
		if err != nil {
			t.Fatal(err)
		}
		for _, setting := range list {
			if setting.Key == "enableWorkflows" {
				return setting
			}
		}
		t.Fatal("workflows setting is missing")
		return ClaudeSetting{}
	}
	setting := read()
	// Claude's schema leaves the default to the account's plan, not a fixed bool.
	if setting.Type != "bool" || setting.Default != nil || setting.Value != nil {
		t.Fatalf("unset setting = %+v", setting)
	}
	if err := n.setClaudeSetting("enableWorkflows", "true"); err == nil {
		t.Fatal("non-boolean workflows value accepted")
	}
	for _, value := range []any{true, false, nil} {
		if err := n.setClaudeSetting("enableWorkflows", value); err != nil {
			t.Fatal(err)
		}
		if got := read().Value; got != value {
			t.Fatalf("value = %v, want %v", got, value)
		}
	}
}
