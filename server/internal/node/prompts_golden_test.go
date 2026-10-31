package node

// Golden test for the message the node writes into an assistant conversation (docs/prompts.md). Regenerate after an
// intended change with UPDATE_GOLDEN=1 go test ./internal/node -run Golden, and read the diff.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goldenNode(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || got != string(want) {
		t.Errorf("%s: got\n%s\nwant (%v)\n%s", name, got, err, want)
	}
}

// The memory section of the Codex assistant's developer instructions: no index yet, an index, one too long to show.
func TestGoldenCodexMemory(t *testing.T) {
	dir := t.TempDir()
	goldenNode(t, "codex_memory.empty", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("\n- [邮箱](mail.md) — 收信在 hub 上\n- [习惯](habits.md) — 回复先给结论\n\n"), 0o600)
	goldenNode(t, "codex_memory.index", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("  "+strings.Repeat("- [条目](x.md) — 说明\n", 1200)), 0o600)
	goldenNode(t, "codex_memory.long", strings.ReplaceAll(codexMemoryPrompt(dir), dir, "<dir>"))
}

func TestGoldenLostTasksNotice(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-000000000001"
	n.state.put(id, sessionState{Cwd: "/home/u/.jarvis/assistant", Tasks: []string{"tail the deploy log", "Sleep 23 minutes"}})
	var got string
	n.recoverAssistantTasks(func(_, text string) error { got = text; return nil })
	path := filepath.Join("testdata", "prompts", "lost_tasks.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil || got != string(want) {
		t.Fatalf("got\n%s\nwant (%v)\n%s", got, err, want)
	}
}
