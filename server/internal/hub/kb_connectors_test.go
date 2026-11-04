package hub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
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

// pollFake is an archive that asks to be polled every minute and counts the calls.
type pollFake struct{ calls int }

func (*pollFake) ID() string   { return "example" }
func (*pollFake) Name() string { return "Example" }
func (*pollFake) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapArchive}, Poll: time.Minute}
}
func (f *pollFake) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	f.calls++
	return nil, cursor, false, nil
}

// A connector is asked when it is due (its manifest's Poll), not on every loop of the knowledge base.
func TestKBConnectorsWhenDue(t *testing.T) {
	h := newTestHub(t)
	st, _ := kb.NewStore(t.TempDir())
	h.kb.eng = &kb.Engine{Store: st}
	h.connectors = connector.NewRegistry(nil)
	f := &pollFake{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	h.kbConnectors(context.Background())
	h.kbConnectors(context.Background())
	if f.calls != 1 {
		t.Fatalf("asked %d times within its minute", f.calls)
	}
	h.kb.archivedAt["example"] = time.Now().Add(-2 * time.Minute)
	h.kbConnectors(context.Background())
	if f.calls != 2 {
		t.Fatalf("not asked again once due: %d", f.calls)
	}
}

// Mail's cursors from before (mail-cursors.json) are where its archive goes on from: nothing archived twice; the
// new file takes over, the old one stays.
func TestMailCursorGoesOnFromTheOldFile(t *testing.T) {
	st, _ := kb.NewStore(t.TempDir())
	e := &kb.Engine{Store: st}
	old := `{"acc1":12}`
	if err := os.WriteFile(filepath.Join(st.Dir, "mail-cursors.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &archiveFake{}
	if _, err := archiveConnector(context.Background(), e, "mail", f); err != nil {
		t.Fatal(err)
	}
	if f.cursors[0] != old {
		t.Fatalf("started from %q, not the old cursors", f.cursors[0])
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "cursors", "mail.json")); err != nil {
		t.Fatal("no new cursor file")
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "mail-cursors.json")); err != nil {
		t.Fatal("the old file is gone")
	}
	// Another connector never reads mail's old file.
	g := &archiveFake{}
	if _, err := archiveConnector(context.Background(), e, "example", g); err != nil || g.cursors[0] != "" {
		t.Fatalf("example started from %q, %v", g.cursors[0], err)
	}
}

// mediaArchive hands out one message with two attachments, one of which it can fetch (connector.ArchiveMedia).
type mediaArchive struct{ asked []string }

func (*mediaArchive) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	if cursor != "" {
		return nil, cursor, false, nil
	}
	return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "g1", Name: "示例群", Kind: "group"},
		Messages: []kb.SourceMessage{{ID: "m1", SenderName: "示例用户", Type: "FILE", Text: "周报", At: time.Now().Add(-time.Hour),
			Media: []string{"file:ok", "file:gone"}}}}}, "1", false, nil
}

func (a *mediaArchive) ArchiveMedia(_ context.Context, ref string) (connector.Media, error) {
	a.asked = append(a.asked, ref)
	if ref != "file:ok" {
		return connector.Media{}, os.ErrNotExist
	}
	return connector.Media{Mime: "application/pdf", Data: []byte("%PDF-1.7")}, nil
}

// An archived message's attachments come from the connector (archive.media): kept next to the chat and linked from
// the line; one it cannot hand over stays [附件].
func TestArchiveConnectorKeepsAttachments(t *testing.T) {
	st, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &mediaArchive{}
	if _, err := archiveConnector(context.Background(), &kb.Engine{Store: st}, "example", a); err != nil {
		t.Fatal(err)
	}
	c, _ := st.Chat(kb.ChatID("example", "g1"))
	text := st.Since(c.ID, time.Time{}, 10000)
	if len(a.asked) != 2 || !strings.Contains(text, "[附件](attachments/") || strings.Count(text, "[附件]") != 2 {
		t.Fatalf("asked %v, archived:\n%s", a.asked, text)
	}
	var kept []string
	_ = filepath.Walk(filepath.Join(st.Dir), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.Contains(p, "attachments") {
			kept = append(kept, p)
		}
		return nil
	})
	if len(kept) != 1 {
		t.Fatalf("attachments kept: %v", kept)
	}
	if b, _ := os.ReadFile(kept[0]); string(b) != "%PDF-1.7" {
		t.Fatalf("attachment holds %q", b)
	}
}
