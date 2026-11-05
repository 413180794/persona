package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// sourceExample has an archive (polled every minute, filing under "posts" too, with attachments) and items.
type sourceExample struct{ asked []string }

func (*sourceExample) ID() string   { return "example" }
func (*sourceExample) Name() string { return "Example" }
func (*sourceExample) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapArchive, connector.CapItems}, Poll: time.Minute,
		ArchiveSources: []string{"posts", "mail"}}
}
func (s *sourceExample) Changes(_ context.Context, cursor string, limit int) ([]connector.ChatBatch, string, bool, error) {
	s.asked = append(s.asked, "changes "+cursor)
	return []connector.ChatBatch{{Chat: kb.SourceChat{ID: "g1", Name: "示例群", Kind: "group"},
		Messages: []kb.SourceMessage{{ID: "m1", SenderName: "示例用户", Type: "TEXT", Text: "周报", At: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)}}}}, "2", true, nil
}
func (s *sourceExample) ArchiveMedia(_ context.Context, ref string) (connector.Media, error) {
	s.asked = append(s.asked, "media "+ref)
	if ref == "big" {
		return connector.Media{Data: make([]byte, connector.ArchiveMediaMost+1)}, nil
	}
	return connector.Media{Mime: "application/pdf", Data: []byte("%PDF-1.7")}, nil
}
func (s *sourceExample) ItemSource() kb.ItemSource { return exampleItems{} }

type exampleItems struct{}

func (exampleItems) Items(context.Context) ([]kb.Item, error) {
	return []kb.Item{{Kind: kb.KindMeeting, ID: "m1", Title: "周会"}}, nil
}
func (exampleItems) DocTitle(context.Context, string) string { return "发布计划" }

// The knowledge base's sources are the hub's connectors, read through it as data (connectors.*): which to read now
// (signed in, 进知识库 on: its poll, its other names but another connector's id, whether it has attachments), what moved
// since a cursor, an attachment (not a too big one), the items and a document's title.
func TestKBSourcesAreTheHubsConnectors(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.connectors = connector.NewRegistry(nil)
	f := &sourceExample{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	if err := h.addConnector(namedConnector("mail")); err != nil { // another connector: "mail" is its id, not one of example's names
		t.Fatal(err)
	}
	ask := func(method, params string) (string, error) {
		t.Helper()
		raw, ok, err := h.kbSourcesCall(ctx, assistantwire.AppCall{AppID: assistantwire.AppConnectors, Method: method, Params: json.RawMessage(params)})
		if !ok {
			t.Fatalf("%s not answered", method)
		}
		return string(raw), err
	}
	out, err := ask(assistantwire.ConnectorsArchives, `{}`)
	if err != nil || !strings.Contains(out, `{"id":"example","poll_ms":60000,"sources":["posts"],"media":true}`) || !strings.Contains(out, `"items":["example"]`) {
		t.Fatalf("archives: %s %v", out, err)
	}
	if out, err := ask(assistantwire.ConnectorsChanges, `{"id":"example","cursor":"1","limit":20}`); err != nil ||
		!strings.Contains(out, `"next":"2","more":true`) || !strings.Contains(out, `"text":"周报"`) || f.asked[0] != "changes 1" {
		t.Fatalf("changes: %s %v", out, err)
	}
	if out, err := ask(assistantwire.ConnectorsArchiveMedia, `{"id":"example","ref":"file:1"}`); err != nil || out != `{"media":{"mime":"application/pdf","data":"JVBERi0xLjc="}}` {
		t.Fatalf("media: %s %v", out, err)
	}
	if _, err := ask(assistantwire.ConnectorsArchiveMedia, `{"id":"example","ref":"big"}`); err == nil {
		t.Fatal("a too big attachment was handed over")
	}
	if out, err := ask(assistantwire.ConnectorsItems, `{"id":"example"}`); err != nil || !strings.Contains(out, `"title":"周会"`) {
		t.Fatalf("items: %s %v", out, err)
	}
	if out, err := ask(assistantwire.ConnectorsDocTitle, `{"id":"example","url":"https://docs.example.com/d1"}`); err != nil || out != `{"title":"发布计划"}` {
		t.Fatalf("doc title: %s %v", out, err)
	}
	if _, err := ask(assistantwire.ConnectorsChanges, `{"id":"nope"}`); err == nil {
		t.Fatal("changes of a connector the hub does not have")
	}

	// 进知识库 off: not listed, and its changes are not read.
	if err := h.connectors.SetFeed("example", connector.FeedArchive, false); err != nil {
		t.Fatal(err)
	}
	if out, _ := ask(assistantwire.ConnectorsArchives, `{}`); strings.Contains(out, `"id":"example"`) {
		t.Fatalf("listed with 进知识库 off: %s", out)
	}
	if _, err := ask(assistantwire.ConnectorsChanges, `{"id":"example"}`); err == nil || len(f.asked) != 3 {
		t.Fatalf("read with 进知识库 off: %v %v", err, f.asked)
	}
	// Another app's methods are not these.
	if _, ok, _ := h.kbSourcesCall(ctx, assistantwire.AppCall{AppID: assistantwire.AppConnectors, Method: assistantwire.ConnectorsSignedIn}); ok {
		t.Fatal("connectors.signed_in taken here")
	}
}
