package hub

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// replyRacingConnector is an inbox connector whose Reply runs a hook first: what collection does while a reply is out.
type replyRacingConnector struct {
	inboxConnector
	during func()
}

func (c *replyRacingConnector) Reply(ctx context.Context, r connector.InboxReply) error {
	if c.during != nil {
		c.during()
		c.during = nil
	}
	return c.inboxConnector.Reply(ctx, r)
}

// A move that comes into a connector's item while the reply is out keeps the item open, so it is not filed unseen;
// the same request retried (no client_id, as the web and the Apple apps send) still goes out once; the user's answer
// to the new move files it (#1828).
func TestReplyKeepsMovesThatCameInMeanwhile(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	now := time.Now()
	c := &replyRacingConnector{inboxConnector: inboxConnector{things: []connector.InboxThing{thing("a", now.Add(-time.Minute))}}}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	h.collectConnectors(ctx)
	open := openItems(h)
	if len(open) != 1 {
		t.Fatalf("open items: %d", len(open))
	}
	it := open[0]
	c.during = func() { // bob writes again while the reply is out; collection gathers it into the open item
		c.things = []connector.InboxThing{thing("b", now)}
		h.collectConnectors(ctx)
	}

	out, err := h.replyInboxAs(ctx, it.ID, "看过了", "")
	if err != nil {
		t.Fatal(err)
	}
	if !it.open() || it.Last != "b" || it.DoneBy != "" || out.DoneAt != nil || it.Count != 2 {
		t.Fatalf("the move that came in meanwhile was filed with the reply: %+v", it)
	}
	if len(openItems(h)) != 1 {
		t.Fatalf("a new item instead of the open one: %d open", len(openItems(h)))
	}

	// The request timed out on the phone and is sent again: the reply already went out.
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if len(c.replies) != 1 || !it.open() {
		t.Fatalf("a retry went out again: %v, item %+v", c.replies, it)
	}

	// The user reads the new move and answers it: sent, and nothing came in meanwhile, so the item is filed.
	if _, err := h.replyInboxAs(ctx, it.ID, "好，我改", ""); err != nil {
		t.Fatal(err)
	}
	if len(c.replies) != 2 || c.replies[1] != "example-org/example-repo#12: 好，我改" || it.open() || it.DoneBy != "replied" {
		t.Fatalf("the answer to the new move: %v, item %+v", c.replies, it)
	}
	// And its retry is dropped as before (filed as replied).
	if _, err := h.replyInboxAs(ctx, it.ID, "好，我改", ""); err != nil || len(c.replies) != 2 {
		t.Fatalf("retry after filing: %v %v", err, c.replies)
	}
}

// An item filed meanwhile (read elsewhere) is filed as replied, as before; one that took no new move is filed.
func TestReplyFilesWhenNothingNewCameIn(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	now := time.Now()
	c := &replyRacingConnector{inboxConnector: inboxConnector{things: []connector.InboxThing{thing("a", now.Add(-time.Minute))}}}
	if err := h.addConnector(c); err != nil {
		t.Fatal(err)
	}
	h.collectConnectors(ctx)
	it := openItems(h)[0]
	c.during = func() { // read on GitHub meanwhile, with a newer move: collection files it as read
		c.things = []connector.InboxThing{thing("b", now)}
		c.things[0].Tier, c.things[0].Read = TierFYI, true
		h.collectConnectors(ctx)
		if it.open() {
			t.Error("the test's collection did not file the item")
		}
	}
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if it.open() || it.DoneBy != "replied" {
		t.Fatalf("filed meanwhile: %+v", it)
	}
}

// The same for a Feishu item a plugin took over: the new message keeps it open, the retry keeps the hub's client id
// and is not sent again; a request with its own client_id is another reply (#1828).
func TestMigratedReplyKeepsMessagesThatCameInMeanwhile(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	h.connectors = connector.NewRegistry(nil)
	p := &extensionLark{}
	racing := &extensionLarkRacing{extensionLark: p}
	if err := h.addConnector(racing); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	it := &InboxItem{ID: "lark:10:3", Source: "lark", Kind: KindDM, Account: "feishu", AccountName: "飞书", Title: "Alice", Last: "3", Count: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Lark: &InboxLark{AccountID: 1, FeedID: "10", StartPosition: "3"}}
	h.inbox.items[it.ID] = it
	racing.during = func() { // Alice writes again while the reply is out
		h.inbox.mu.Lock()
		it.Last, it.Count, it.Preview = "5", 2, "还有一件事"
		h.inbox.mu.Unlock()
	}

	if _, err := h.replyInbox(ctx, it.ID, "在的"); err != nil {
		t.Fatal(err)
	}
	if !it.open() || it.DoneBy != "" || len(p.got) != 1 {
		t.Fatalf("filed with Alice's new message: %+v, sent %d", it, len(p.got))
	}
	if _, err := h.replyInbox(ctx, it.ID, "在的"); err != nil || len(p.got) != 1 {
		t.Fatalf("a retry went out again: %v, sent %d", err, len(p.got))
	}

	// A device that names its sends: another id is another reply, even with the same words.
	const id = "6f1c2b3a-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	if _, err := h.replyInboxAs(ctx, it.ID, "在的", id); err != nil {
		t.Fatal(err)
	}
	if len(p.got) != 2 || p.got[1].ClientID != id || it.open() || it.DoneBy != "replied" {
		t.Fatalf("a reply of its own: %+v, item %+v", p.got, it)
	}
}

// extensionLarkRacing is extensionLark with a hook run before Reply records.
type extensionLarkRacing struct {
	*extensionLark
	during func()
}

func (c *extensionLarkRacing) Reply(ctx context.Context, r connector.InboxReply) error {
	if c.during != nil {
		c.during()
		c.during = nil
	}
	return c.extensionLark.Reply(ctx, r)
}
