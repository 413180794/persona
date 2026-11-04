package hub

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
)

// pluginLark stands in for a plugin's connector "lark": an inbox that records replies.
type pluginLark struct {
	things []connector.InboxThing
	got    []connector.InboxReply
}

func (*pluginLark) ID() string   { return "lark" }
func (*pluginLark) Name() string { return "飞书" }
func (*pluginLark) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}, Kinds: []string{KindDM, KindDoc}}
}
func (c *pluginLark) Collect(context.Context) ([]connector.InboxThing, error) { return c.things, nil }
func (c *pluginLark) Reply(_ context.Context, r connector.InboxReply) error {
	c.got = append(c.got, r)
	return nil
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// A plugin takes the hub's own Feishu connector over and gives it back: the id stays, the hub's comes back when the
// plugin's goes.
func TestPluginTakesOverLark(t *testing.T) {
	h := newTestHub(t)
	h.lark = lark.New(lark.Config{Token: "t"})
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	if _, own := h.connectors.Get("lark").(larkConnector); !own || h.larkByPlugin() {
		t.Fatal("the hub's own Feishu connector first")
	}
	p := &pluginLark{}
	h.yieldOwnConnector("lark")
	if err := h.addConnector(p); err != nil {
		t.Fatal(err)
	}
	if h.connectors.Get("lark") != p || !h.larkByPlugin() {
		t.Fatal("the plugin's is the one now")
	}
	h.removePluginConnector("lark")
	if _, own := h.connectors.Get("lark").(larkConnector); !own || h.larkByPlugin() {
		t.Fatal("the hub's own is back")
	}
	h.yieldOwnConnector("github") // not migratable: stays
	if h.connectors.Get("github") == nil {
		t.Fatal("github is not a plugin's to take")
	}
}

// The things of a plugin that took Feishu over keep the items the hub made itself: the same item goes on (its start
// stays), new ones get the hub's own shape and id, read and replied end them as before, a re-read document's rows
// update the item; replies and status moves go to the plugin with the hub's client id.
func TestMigratedInbox(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	h.lark = lark.New(lark.Config{Token: "t"})
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	p := &pluginLark{}
	h.yieldOwnConnector("lark")
	if err := h.addConnector(p); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := &InboxItem{ID: "lark:10:3", Source: "lark", Kind: KindDM, Account: "feishu", AccountName: "飞书", Title: "Alice", Last: "3", Count: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Lark: &InboxLark{AccountID: 1, FeedID: "10", StartPosition: "3"}}
	h.inbox.items[old.ID] = old
	extra := func(block string, v any, legacy string, count int) *connector.ThingExtra {
		x := &connector.ThingExtra{Source: "lark", Account: "feishu", AccountName: "飞书", LegacyID: legacy, Count: count, StartedAt: now}
		switch block {
		case "lark":
			x.Lark = rawJSON(v)
		case "doc":
			x.Doc = rawJSON(v)
		}
		return x
	}
	p.things = []connector.InboxThing{
		{Thread: "chat:10", Kind: KindDM, Title: "Alice", Preview: "还在吗", At: now, Last: "5", CanReply: true,
			Extra: extra("lark", InboxLark{AccountID: 1, FeedID: "10", StartPosition: "4", ReplyTo: "m5"}, "lark:10:4", 2)},
		{Thread: "doc:n1", Kind: KindDoc, Title: "周报", At: now, Last: "100",
			Extra: extra("doc", InboxDoc{NoticeID: "n1", Type: "sheet", Rows: []DocRow{{Ref: "t!1", Title: "行", Status: []DocStatus{{Field: "2", Name: "状态", Value: "进行中",
				Options: []DocOption{{Name: "进行中"}, {Name: "完成"}}}}}}}, "doc:n1:100", 1)},
	}
	h.collectConnectors(ctx)
	if len(h.inbox.items) != 2 {
		t.Fatalf("items: %d", len(h.inbox.items))
	}
	if old.Last != "5" || old.Count != 2 || old.Lark.StartPosition != "3" || old.Lark.ReplyTo != "m5" || old.Connector != nil || old.Preview != "还在吗" {
		t.Fatalf("the hub's own item goes on: %+v %+v", old, old.Lark)
	}
	doc := h.inbox.items["doc:n1:100"]
	if doc == nil || doc.Doc == nil || doc.Connector != nil || doc.Account != "feishu" || len(doc.Doc.Rows) != 1 {
		t.Fatalf("a new item in the hub's own shape: %+v", doc)
	}

	// The same move, the document read again: its rows change.
	p.things[1].Extra = extra("doc", InboxDoc{NoticeID: "n1", Type: "sheet", Rows: []DocRow{{Ref: "t!1", Title: "行", Status: []DocStatus{{Field: "2", Name: "状态", Value: "完成",
		Options: []DocOption{{Name: "进行中"}, {Name: "完成"}}}}}}}, "doc:n1:100", 1)
	h.collectConnectors(ctx)
	if doc.Doc.Rows[0].Status[0].Value != "完成" {
		t.Fatalf("rows not updated: %+v", doc.Doc.Rows)
	}

	// A reply goes to the plugin with the client id the hub would have used itself; a status move as changes.
	if _, err := h.replyInbox(ctx, "lark:10:3", "在的"); err != nil {
		t.Fatal(err)
	}
	want := lark.NamedClientID("lark:10:3" + "\n" + "在的")
	if len(p.got) != 1 || p.got[0].Thread != "chat:10" || p.got[0].ClientID != want || p.got[0].Text != "在的" || old.DoneBy != "replied" {
		t.Fatalf("reply: %+v, item done by %q", p.got, old.DoneBy)
	}
	if _, err := h.setDocStatus(ctx, "doc:n1:100", DocChange{Ref: "t!1", Field: "2", Value: "进行中"}); err != nil {
		t.Fatal(err)
	}
	if len(p.got) != 2 || p.got[1].Thread != "doc:n1" || len(p.got[1].Changes) != 1 || p.got[1].Changes[0].Value != "进行中" || p.got[1].Text != "" {
		t.Fatalf("status move: %+v", p.got)
	}
	if _, err := h.setDocStatus(ctx, "doc:n1:100", DocChange{Ref: "t!1", Field: "2", Value: "没有这个"}); err != errNoDocRow {
		t.Fatalf("a value the column does not offer: %v", err)
	}

	// Read in Feishu: the doc (untriaged) is read; replied: the chat's next item ends.
	p.things[1].Read = true
	h.collectConnectors(ctx)
	if !doc.Read {
		t.Fatal("read elsewhere")
	}
}
