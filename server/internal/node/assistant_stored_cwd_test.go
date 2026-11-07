package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
)

// A conversation resumed without a folder runs where it ran before; when that folder is gone (the data directory
// moved), in the folder of the same name in the data directory, else the assistant directory (#6299).
func TestStoredCwd(t *testing.T) {
	t.Setenv("PERSONA_HOME", "")
	n := codexTestNode(t)
	data := identity.DataDir(n.home)
	for _, d := range []string{"assistant", "health"} {
		if err := os.MkdirAll(filepath.Join(data, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gone := filepath.Join(t.TempDir(), ".previous")
	kept := t.TempDir()
	cases := []struct{ name, stored, want string }{
		{"a stored folder that exists", kept, kept},
		{"the assistant directory moved", filepath.Join(gone, "assistant"), filepath.Join(data, "assistant")},
		{"the health space moved", filepath.Join(gone, "health"), filepath.Join(data, "health")},
		{"gone with nothing of its name", filepath.Join(gone, "elsewhere"), identity.AssistantDir(n.home)},
		{"none stored", "", identity.AssistantDir(n.home)},
	}
	for _, c := range cases {
		if got := n.storedCwd(c.stored); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Errorf("the gone folder was made again: %v", err)
	}
}

// The main conversation whose stored folder is gone starts in the assistant directory of the data directory it moved
// to, and nothing is made at the old path.
func TestAssistantResumesInTheMovedFolder(t *testing.T) {
	fakeClaudeHome(t, "pwd -P > \"$HOME/cwd.txt\"\nread line\necho '{\"type\":\"system\",\"subtype\":\"init\"}'\nsleep 30\n")
	t.Setenv("PERSONA_HOME", "")
	fake := os.Getenv("HOME")
	n := codexTestNode(t)
	withFakeHub(t, n)
	moved := identity.AssistantDir(n.home)
	if err := os.MkdirAll(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), ".previous", "assistant")
	id := newUUID()
	n.state.put(id, sessionState{Launcher: "claude", Cwd: old})

	if _, err := n.assistantSend(context.Background(), id, "", "claude", "hello", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	defer n.stopAssistant(id)
	var ran string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(fake, "cwd.txt")); err == nil && len(b) > 0 {
			ran = strings.TrimSpace(string(b))
			break
		}
	}
	want, _ := filepath.EvalSymlinks(moved)
	if ran != want {
		t.Fatalf("the conversation ran in %q, want %q", ran, want)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("the old folder was made again: %v", err)
	}
	if st, _ := n.state.get(id); st.Cwd != moved {
		t.Fatalf("stored folder %q, want it corrected to %q", st.Cwd, moved)
	}
}
