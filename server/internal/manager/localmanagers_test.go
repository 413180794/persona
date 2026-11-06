package manager

import (
	"os"
	"path/filepath"
	"testing"
)

// The built-in manager's directory (managers/assistant, only its configuration) is not a manager to run or list.
func TestLocalManagersLeaveTheBuiltInOneOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", home)
	for _, id := range []string{"assistant", "work"} {
		d := filepath.Join(home, "managers", id)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "manager.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := localManagers()
	if len(got) != 1 || filepath.Base(got[0]) != "work" {
		t.Fatalf("local managers %v", got)
	}
}
