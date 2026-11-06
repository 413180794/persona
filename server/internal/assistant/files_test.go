package assistant

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/managerconf"
)

// The built-in manager reads its keys in its own directory once they are copied there, never the hub's in the data
// directory; its knowledge base stays the data directory's data/ unless its manager.json says otherwise.
func TestTheBuiltinManagerUsesItsOwnFiles(t *testing.T) {
	home := t.TempDir()
	for _, name := range managerconf.OwnFiles {
		if err := os.WriteFile(filepath.Join(home, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var said []string
	moveFiles(home, func(format string, args ...any) { said = append(said, format) })
	if len(said) != 1 {
		t.Fatalf("what the move said: %q", said)
	}
	own := managerconf.DefaultDir(home)
	o := builtinFiles(home)
	for what, got := range map[string]string{"secret.key": o.SecretKey, "tailcat key": o.TailcatKey,
		"handles.key's directory": filepath.Join(o.HandlesDir, managerconf.HandlesKey)} {
		if filepath.Dir(got) != own {
			t.Errorf("%s: %s, not in %s", what, got, own)
		}
	}
	if o.KBDir != filepath.Join(home, "data") {
		t.Errorf("knowledge base %s", o.KBDir)
	}
	if o.Dir != home || o.ConfigFile != managerconf.DefaultFile(home) {
		t.Errorf("data directory %s, manager.json %s", o.Dir, o.ConfigFile)
	}
}
