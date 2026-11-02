package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
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
	if session.Status != proto.StatusOffline || session.Cwd != "/code/project" || session.Title != "自定义名称" || !session.UpdatedAt.Equal(activity) {
		t.Fatalf("history metadata = %+v", session)
	}
}
