package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSettingsKeepOtherKeys(t *testing.T) {
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
	var raw map[string]any
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &raw)
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
