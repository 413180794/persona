package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestOfflineSessionHistoryMetadata(t *testing.T) {
	n, profile := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	const id = "cccccccc-0000-0000-0000-000000000003"
	activity := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	user, err := json.Marshal(map[string]any{
		"type": "user", "uuid": "user", "cwd": "/code/project", "timestamp": activity,
		"message": map[string]any{"role": "user", "content": "原始问题"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data := string(user) + "\n" + `{"type":"custom-title","customTitle":"自定义名称","sessionId":"` + id + `"}` + "\n" + strings.Repeat(" ", 4096) + "\n"
	path := filepath.Join(profile, "projects", "-src-app", id+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var history []proto.Session
	for _, session := range n.scanOffline() {
		if session.SessionID == id {
			history = append(history, session)
		}
	}
	if len(history) != 1 {
		t.Fatalf("history = %+v", history)
	}
	session := history[0]
	if session.Status != proto.StatusOffline || session.Agent != "claude" || session.Cwd != "/code/project" || session.Title != "自定义名称" || !session.UpdatedAt.Equal(activity) {
		t.Fatalf("history metadata = %+v", session)
	}
}

func TestOfflineSessionHistoryActivity(t *testing.T) {
	n, profile := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	const id = "cccccccc-0000-0000-0000-000000000004"
	now := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	toolCall, err := json.Marshal(map[string]any{
		"type": "assistant", "uuid": "a1", "timestamp": now,
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "/code/project/todos.ts"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	titleLine := `{"type":"custom-title","customTitle":"读任务","sessionId":"` + id + `"}`
	data := string(toolCall) + "\n" + titleLine + "\n" + strings.Repeat(" ", 4096) + "\n"
	path := filepath.Join(profile, "projects", "-src-app", id+".jsonl")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var history []proto.Session
	for _, session := range n.scanOffline() {
		if session.SessionID == id {
			history = append(history, session)
		}
	}
	if len(history) != 1 {
		t.Fatalf("history = %+v", history)
	}
	s := history[0]
	if s.Agent != "claude" || s.Status != proto.StatusOffline {
		t.Fatalf("agent/status = %q/%q", s.Agent, s.Status)
	}
	if s.Activity != "在读 todos.ts" || s.ActivityAt == nil || !s.ActivityAt.Equal(now) {
		t.Fatalf("activity = %q, at = %v", s.Activity, s.ActivityAt)
	}
}
