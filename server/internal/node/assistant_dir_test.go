package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// The hub names the assistant's directories relative to HOME (".persona/assistant", ".persona/health", ".persona/guest").
// Under a custom PERSONA_HOME they are in that root, where the hub says the assistant is (the node's data_dir, #5505):
// nothing is written into the default ~/.persona. On the default layouts they stay where they always were.
func TestAssistantDirsFollowTheDataDir(t *testing.T) {
	t.Setenv("PERSONA_SKILLS_DIR", t.TempDir())
	prepare := func(t *testing.T, n *Node, method, dir string) string {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"dir": dir, "claude_md": "# example"})
		out, err := n.handle(context.Background(), method, params)
		if err != nil {
			t.Fatal(err)
		}
		cwd, _ := out.(map[string]any)["cwd"].(string)
		if _, err := os.Stat(filepath.Join(cwd, "CLAUDE.md")); err != nil {
			t.Fatalf("%s: no CLAUDE.md in %s: %v", dir, cwd, err)
		}
		return cwd
	}
	cases := []struct{ method, dir, sub string }{
		{"assistant.prepare", ".persona/assistant", "assistant"},
		{"assistant.prepare", ".persona/health", "health"},
		{"assistant.guest_prepare", ".persona/guest", "guest"},
	}

	t.Run("custom PERSONA_HOME", func(t *testing.T) {
		n, home := testNode(t)
		root := filepath.Join(t.TempDir(), "root")
		t.Setenv("PERSONA_HOME", root)
		n.dir, n.logger = root, log.New(io.Discard, "", 0)
		for _, c := range cases {
			if cwd := prepare(t, n, c.method, c.dir); cwd != filepath.Join(root, c.sub) {
				t.Errorf("%s: prepared %s, want %s", c.dir, cwd, filepath.Join(root, c.sub))
			}
		}
		if _, err := os.Stat(filepath.Join(home, ".persona")); !os.IsNotExist(err) {
			t.Errorf("the default state directory was written: %v", err)
		}
	})

	for _, def := range []string{".persona"} {
		t.Run("default "+def, func(t *testing.T) {
			n, home := testNode(t)
			t.Setenv("PERSONA_HOME", "")
			n.dir, n.logger = filepath.Join(home, def), log.New(io.Discard, "", 0)
			for _, c := range cases {
				if cwd := prepare(t, n, c.method, c.dir); cwd != filepath.Join(home, c.dir) {
					t.Errorf("%s: prepared %s, want it where it always was, %s", c.dir, cwd, filepath.Join(home, c.dir))
				}
			}
		})
	}
}
