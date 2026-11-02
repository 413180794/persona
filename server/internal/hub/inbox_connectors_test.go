package hub

import (
	"context"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// inboxConnector is a connector with an inbox, on made-up data.
type inboxConnector struct {
	things  []connector.InboxThing
	replies []string
}

func (*inboxConnector) ID() string   { return "example" }
func (*inboxConnector) Name() string { return "Example" }
func (c *inboxConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	return c.things, nil
}
func (c *inboxConnector) Reply(_ context.Context, thread, text string) error {
	c.replies = append(c.replies, thread+": "+text)
	return nil
}

func thing(last string, at time.Time) connector.InboxThing {
	return connector.InboxThing{Thread: "example-org/example-repo#12", Kind: "review_requested", Label: "请你 review",
		Title: "Add the example", Sender: "bob", Preview: "请看一下", Context: "上下文", At: at, Last: last,
		URL: "https://github.com/example-org/example-repo/pull/12", ReplyTo: "评论到 example-org/example-repo#12", CanReply: true, Tier: TierAction}
}

func openItems(h *Hub) []*InboxItem {
	var out []*InboxItem
	for _, it := range h.inbox.items {
		if it.open() {
			out = append(out, it)
		}
	}
	return out
}

// One open item per thread gathers its moves; done, a later move starts a new item, the same one does not; a thing
// that is over closes the open item.
func TestConnectorThingsBecomeItems(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	now := time.Now()
	apply := func(things ...connector.InboxThing) []*InboxItem {
		h.inbox.mu.Lock()
		defer h.inbox.mu.Unlock()
		return h.inbox.applyConnector("example", "Example", things, now)
	}

	if ch := apply(thing("a", now.Add(-time.Hour))); len(ch) != 1 {
		t.Fatalf("changed %d", len(ch))
	}
	it := openItems(h)[0]
	if it.Source != "example" || it.AccountName != "Example" || it.Connector.Label != "请你 review" || !it.Connector.CanReply || it.Count != 1 {
		t.Fatalf("%+v %+v", it, it.Connector)
	}
	if ruleTier(it) != TierAction {
		t.Fatalf("rule tier %s", ruleTier(it))
	}
	if ch := apply(thing("a", now.Add(-time.Hour))); len(ch) != 0 {
		t.Fatal("the same move changed the item")
	}
	apply(thing("b", now.Add(-time.Minute)))
	if len(openItems(h)) != 1 || it.Count != 2 || it.Last != "b" {
		t.Fatalf("a later move did not join the open item: %d open, count %d", len(openItems(h)), it.Count)
	}

	it.done(now, "user")
	apply(thing("b", now.Add(-time.Minute)))
	if len(openItems(h)) != 0 {
		t.Fatal("a move already dealt with came back")
	}
	apply(thing("c", now))
	if len(openItems(h)) != 1 || openItems(h)[0] == it {
		t.Fatal("a move after done did not start a new item")
	}

	over := thing("d", now)
	over.Done = connector.DoneRead
	apply(over)
	if len(openItems(h)) != 0 {
		t.Fatal("a thread that is over left its item open")
	}

	// Old or already read: never an item.
	old := thing("e", now.Add(-inboxWindow-time.Hour))
	old.Thread = "example-org/example-repo#1"
	read := thing("f", now)
	read.Thread, read.Read = "example-org/example-repo#2", true
	if ch := apply(old, read); len(ch) != 0 {
		t.Fatalf("old or read things became items: %d", len(ch))
	}
}

// Read elsewhere: an item that does not need the user is done; one that does stays.
func TestConnectorThingReadElsewhere(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	now := time.Now()
	h.inbox.mu.Lock()
	h.inbox.applyConnector("example", "Example", []connector.InboxThing{thing("a", now)}, now)
	it := openItems(h)[0]
	it.Tier = TierFYI
	read := thing("a", now)
	read.Read = true
	h.inbox.applyConnector("example", "Example", []connector.InboxThing{read}, now)
	h.inbox.mu.Unlock()
	if it.open() || it.DoneBy != "read" {
		t.Fatalf("an FYI read on GitHub is still open: %+v", it)
	}
}

// Old apps never see a connector's items (they would draw a PR as a mail); new ones ask for them. Reply goes to the
// connector once, and files the item as replied.
func TestConnectorItemsListAndReply(t *testing.T) {
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	c := &inboxConnector{things: []connector.InboxThing{thing("a", time.Now())}}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	h.inbox.mu.Lock()
	h.inbox.items["mail:1"] = &InboxItem{ID: "mail:1", Source: "mail", Kind: KindMail, UpdatedAt: time.Now()}
	h.inbox.mu.Unlock()
	if ch := h.collectConnectors(context.Background()); len(ch) != 1 {
		t.Fatalf("collected %d", len(ch))
	}
	mux := h.routes()
	_, old := call(t, mux, "GET", "/api/v1/inbox", h.cfg.Token, "")
	_, all := call(t, mux, "GET", "/api/v1/inbox?sources=all", h.cfg.Token, "")
	if len(old["items"].([]any)) != 1 || len(all["items"].([]any)) != 2 {
		t.Fatalf("old %v\nall %v", old, all)
	}
	id := openItems(h)[0].ID
	if id == "mail:1" {
		id = openItems(h)[1].ID
	}
	for range 2 { // pressed twice (phone and watch): sent once
		code, out := call(t, mux, "POST", "/api/v1/inbox/"+id+"/reply", h.cfg.Token, `{"text":"看过了"}`)
		if code != 200 || out["done_by"] != "replied" {
			t.Fatalf("%d %v", code, out)
		}
	}
	if len(c.replies) != 1 || c.replies[0] != "example-org/example-repo#12: 看过了" {
		t.Fatalf("replies %v", c.replies)
	}
}
