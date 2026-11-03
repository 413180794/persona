package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// feedConnector has an archive and an inbox, and counts what it was asked.
type feedConnector struct{ changes, collects int }

func (*feedConnector) ID() string   { return "example" }
func (*feedConnector) Name() string { return "Example" }
func (*feedConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox, connector.CapArchive}, Kinds: []string{"dm"}}
}
func (f *feedConnector) Changes(_ context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	f.changes++
	return nil, cursor, false, nil
}
func (f *feedConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	f.collects++
	return []connector.InboxThing{{Thread: "c1", Kind: "dm", Title: "示例用户", Preview: "示例消息", At: time.Now(), Last: "1"}}, nil
}
func (*feedConnector) Reply(context.Context, connector.InboxReply) error { return nil }

func listedExample(t *testing.T, h *Hub) map[string]any {
	t.Helper()
	_, out := call(t, h.routes(), "GET", "/api/v1/connectors", h.cfg.Token, "")
	for _, c := range out["connectors"].([]any) {
		if m := c.(map[string]any); m["id"] == "example" {
			return m
		}
	}
	t.Fatalf("example is not listed: %v", out)
	return nil
}

// 进知识库 off: the archive is not asked at all, while the inbox goes on; 进收件箱 off: nothing new comes and what
// came stays. Both are kept for the next start, and only a connector that has the feed can turn it off.
func TestConnectorFeedsTurnedOff(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	h.inbox.init()
	kst, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: kst}
	f := &feedConnector{}
	if err := h.addConnector(f); err != nil {
		t.Fatal(err)
	}
	mux := h.routes()
	if c := listedExample(t, h); c["archive"] != true || c["inbox"] != true {
		t.Fatalf("feeds are not on to begin with: %v", c)
	}

	code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"archive":false}`)
	if code != 200 || out["archive"] != false || out["inbox"] != true || out["enabled"] != true {
		t.Fatalf("archive off: %d %v", code, out)
	}
	h.kbConnectors(ctx)
	if f.changes != 0 {
		t.Fatalf("asked for changes %d time(s) with 进知识库 off", f.changes)
	}
	h.collectConnectors(ctx)
	if f.collects != 1 || len(openItems(h)) != 1 {
		t.Fatalf("the inbox stopped with the archive: %d collect(s), %d item(s)", f.collects, len(openItems(h)))
	}

	if code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"inbox":false}`); code != 200 || out["inbox"] != false {
		t.Fatalf("inbox off: %d %v", code, out)
	}
	h.collectConnectors(ctx)
	if f.collects != 1 {
		t.Fatalf("collected %d time(s) with 进收件箱 off", f.collects)
	}
	if len(openItems(h)) != 1 {
		t.Fatal("what came before 进收件箱 was turned off went away")
	}

	next := newTestHub(t)
	next.store = st
	next.connectors = connector.NewRegistry(nil)
	_ = next.addConnector(&feedConnector{})
	next.restoreConnectorSwitches(ctx)
	if next.connectors.Feeds("example", connector.FeedArchive) || next.connectors.Feeds("example", connector.FeedInbox) {
		t.Fatalf("the next hub forgot the feeds: %s", st.settings[connectorSettingsKey])
	}
	if !next.connectors.Enabled("example") {
		t.Fatal("turning the feeds off switched the connector off")
	}

	if code, out := call(t, mux, "PUT", "/api/v1/connectors/example/settings", h.cfg.Token, `{"archive":true}`); code != 200 || out["archive"] != true {
		t.Fatalf("archive back on: %d %v", code, out)
	}
	h.kbConnectors(ctx)
	if f.changes != 1 {
		t.Fatalf("not archived once back on: %d", f.changes)
	}
	if !strings.Contains(st.settings[connectorSettingsKey], `"archive":{"example":true}`) {
		t.Fatalf("turning it back on is not kept: %s", st.settings[connectorSettingsKey])
	}

	other := newTestHub(t)
	other.connectors = connector.NewRegistry(nil)
	_ = other.addConnector(&exampleConnector{})
	if code, _ := call(t, other.routes(), "PUT", "/api/v1/connectors/example/settings", other.cfg.Token, `{"archive":false}`); code != 400 {
		t.Fatalf("a connector without an archive turned it off: %d", code)
	}
	_, list := call(t, other.routes(), "GET", "/api/v1/connectors", other.cfg.Token, "")
	if c := list["connectors"].([]any)[0].(map[string]any); c["archive"] != nil || c["inbox"] != nil {
		t.Fatalf("feeds listed for a connector without them: %v", c)
	}
}

// 删除已存的知识库内容 removes the connector's chats and people from the knowledge base and turns 进知识库 off for
// good; the connector keeps running and its inbox items stay.
func TestConnectorArchiveDelete(t *testing.T) {
	h := newTestHub(t)
	st := newKeepStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	h.inbox.init()
	_ = h.addConnector(&feedConnector{})
	mux := h.routes()
	kst, err := kb.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.kb.eng = &kb.Engine{Store: kst}
	_ = kst.PutChat(&kb.Chat{ID: "example-c1", Source: "example", Name: "示例群"})
	_ = kst.PutChat(&kb.Chat{ID: "lark-f1", Source: "lark", Name: "需求群"})
	_ = kst.PutPerson(&kb.Person{ID: "example-p1", Source: "example", Name: "示例用户"})
	h.inbox.items["example:c1"] = &InboxItem{ID: "example:c1", Source: "example", Title: "示例群"}

	if code, _ := call(t, mux, "DELETE", "/api/v1/connectors/nope/archive", h.cfg.Token, ""); code != 404 {
		t.Fatalf("an unknown connector: %d", code)
	}
	code, out := call(t, mux, "DELETE", "/api/v1/connectors/example/archive", h.cfg.Token, "")
	if code != 200 || out["chats"] != 1.0 || out["people"] != 1.0 || out["inbox"] != 0.0 {
		t.Fatalf("delete: %d %v", code, out)
	}
	if h.connectors.Feeds("example", connector.FeedArchive) || !strings.Contains(st.settings[connectorSettingsKey], `"archive":{"example":false}`) {
		t.Fatalf("进知识库 not turned off for good: %s", st.settings[connectorSettingsKey])
	}
	if !h.connectors.Enabled("example") || !h.connectors.Feeds("example", connector.FeedInbox) {
		t.Fatal("the connector or its inbox was switched off")
	}
	if _, ok := kst.Chat("example-c1"); ok {
		t.Error("the chat is still there")
	}
	if _, ok := kst.Person("example-p1"); ok {
		t.Error("the person is still there")
	}
	if _, ok := kst.Chat("lark-f1"); !ok {
		t.Error("Feishu's chat is gone")
	}
	if _, ok := h.inbox.items["example:c1"]; !ok {
		t.Error("the inbox item is gone")
	}

	other := newTestHub(t)
	other.connectors = connector.NewRegistry(nil)
	_ = other.addConnector(&exampleConnector{})
	if code, _ := call(t, other.routes(), "DELETE", "/api/v1/connectors/example/archive", other.cfg.Token, ""); code != 409 {
		t.Fatalf("a connector without an archive: %d", code)
	}
}
