package node

// Golden test for the message the node writes into an assistant conversation (docs/prompts.md). Regenerate after an
// intended change with UPDATE_GOLDEN=1 go test ./internal/node -run Golden, and read the diff.

import (
	"os"
	"path/filepath"
	"testing"
)

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
