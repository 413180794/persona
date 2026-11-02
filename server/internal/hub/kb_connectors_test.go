package hub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
)

// archiveFake hands out one batch per cursor step and records the cursors it was given.
type archiveFake struct {
	cursors []string
	fail    bool
}

func (f *archiveFake) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	f.cursors = append(f.cursors, cursor)
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	switch cursor {
	case "":
		return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "example-org-example-repo-12", Name: "example-repo#12 Add the example", Kind: "pr"},
			Messages: []kb.SourceMessage{{ID: "body", SenderID: "alice", SenderName: "alice", Type: "TEXT", Text: "【Add the example】\n示例描述", At: at}}}}, "1", true, nil
	case "1":
		return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "example-org-example-repo-12", Name: "example-repo#12 Add the example", Kind: "pr"},
			Messages: []kb.SourceMessage{{ID: "c1", SenderID: "me", SenderName: "me", Mine: true, Type: "TEXT", Text: "看过了", At: at.Add(time.Hour)}}}}, "2", false, nil
	}
	return nil, cursor, false, nil
}

// A connector's batches become one chat (ingested like mail), its cursor goes on and is kept for the next run.
func TestArchiveConnectorIngestsAndKeepsTheCursor(t *testing.T) {
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &kb.Engine{Store: st}
	f := &archiveFake{}
	n, err := archiveConnector(context.Background(), e, "example", f)
	if err != nil || n != 2 {
		t.Fatalf("%d lines, %v", n, err)
	}
	c, ok := st.Chat(kb.ChatID("example", "example-org-example-repo-12"))
	if !ok || c.Kind != "pr" || c.Messages != 2 || !c.Mine || c.Source != "example" {
		t.Fatalf("chat %+v", c)
	}
	text := st.Since(c.ID, time.Time{}, 10000)
	if !strings.Contains(text, "示例描述") || !strings.Contains(text, "我") {
		t.Fatalf("archived:\n%s", text)
	}
	raw, _ := os.ReadFile(filepath.Join(st.Dir, "cursors", "example.json"))
	if string(raw) != `{"cursor":"2"}` {
		t.Fatalf("cursor file %s", raw)
	}
	if _, err := archiveConnector(context.Background(), e, "example", f); err != nil {
		t.Fatal(err)
	}
	if last := f.cursors[len(f.cursors)-1]; last != "2" {
		t.Fatalf("the next run started from %q", last)
	}
}
