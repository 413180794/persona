package assistant

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// A reply answers what the item held when it was sent (#1828, ported from the hub's inbox_reply_test.go of #2123):
// what collection gathers into the item while the reply is out keeps it open.

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

// openItem is the one open item of h's inbox.
func openItem(t *testing.T, h *inboxHub) *InboxItem {
	t.Helper()
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	open := openItems(h)
	if len(open) != 1 {
		t.Fatalf("open items: %d", len(open))
	}
	return open[0]
}

// stillOpen says, under the inbox's lock, whether it is open, and its Last and DoneBy.
func stillOpen(h *inboxHub, it *InboxItem) (bool, string, string) {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return it.open(), it.Last, it.DoneBy
}

// A move that comes into a connector's item while the reply is out keeps the item open, so it is not filed unseen;
// the same request retried (no client_id, as the web and the Apple apps send) still goes out once; the user's answer
// to the new move files it.
func TestReplyKeepsMovesThatCameInMeanwhile(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	now := time.Now()
	c := &replyRacingConnector{inboxConnector: inboxConnector{things: []connector.InboxThing{thing("a", now.Add(-time.Minute))}}}
	h.addConnector(c, false)
	h.collectInbox(ctx, roundPoll)
	it := openItem(t, h)
	c.during = func() { // bob writes again while the reply is out; collection gathers it into the open item
		c.things = []connector.InboxThing{thing("b", now)}
		h.collectInbox(ctx, roundPoll)
	}

	out, err := h.replyInboxAs(ctx, it.ID, "看过了", "")
	if err != nil {
		t.Fatal(err)
	}
	if open, last, by := stillOpen(h, it); !open || last != "b" || by != "" || out.DoneAt != nil || out.Count != 2 {
		t.Fatalf("the move that came in meanwhile was filed with the reply: %+v", out)
	}
	if got := openItem(t, h); got != it {
		t.Fatal("a new item instead of the open one")
	}

	// The request timed out on the phone and is sent again: the reply already went out.
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if open, _, _ := stillOpen(h, it); len(c.replies) != 1 || !open {
		t.Fatalf("a retry went out again: %v", c.replies)
	}

	// The user reads the new move and answers it: sent, and nothing came in meanwhile, so the item is filed.
	if _, err := h.replyInboxAs(ctx, it.ID, "好，我改", ""); err != nil {
		t.Fatal(err)
	}
	if open, _, by := stillOpen(h, it); len(c.replies) != 2 || c.replies[1] != "example-org/example-repo#12: 好，我改" || open || by != "replied" {
		t.Fatalf("the answer to the new move: %v, open %v, by %q", c.replies, open, by)
	}
	// And its retry is dropped as before (filed as replied).
	if _, err := h.replyInboxAs(ctx, it.ID, "好，我改", ""); err != nil || len(c.replies) != 2 {
		t.Fatalf("retry after filing: %v %v", err, c.replies)
	}
}

// After the manager restarts (its record of the last reply gone), the same retry carries the same send id: the hub
// had sent it, nothing goes out again.
func TestReplyRetryAfterARestartIsNotSentAgain(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	now := time.Now()
	c := &replyRacingConnector{inboxConnector: inboxConnector{things: []connector.InboxThing{thing("a", now.Add(-time.Minute))}}}
	h.addConnector(c, false)
	h.collectInbox(ctx, roundPoll)
	it := openItem(t, h)
	c.during = func() {
		c.things = []connector.InboxThing{thing("b", now)}
		h.collectInbox(ctx, roundPoll)
	}
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	h.inbox.replies.Delete(it.ID) // what a restart forgets
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil || len(c.replies) != 1 {
		t.Fatalf("the retry after a restart: %v, %v", err, c.replies)
	}
}

// An item filed meanwhile (read elsewhere) is filed as replied, as before.
func TestReplyFilesWhenNothingNewCameIn(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	now := time.Now()
	c := &replyRacingConnector{inboxConnector: inboxConnector{things: []connector.InboxThing{thing("a", now.Add(-time.Minute))}}}
	h.addConnector(c, false)
	h.collectInbox(ctx, roundPoll)
	it := openItem(t, h)
	c.during = func() { // read on GitHub meanwhile, with a newer move: collection files it as read
		c.things = []connector.InboxThing{thing("b", now)}
		c.things[0].Tier, c.things[0].Read = TierFYI, true
		h.collectInbox(ctx, roundPoll)
		if open, _, _ := stillOpen(h, it); open {
			t.Error("the test's collection did not file the item")
		}
	}
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if open, _, by := stillOpen(h, it); open || by != "replied" {
		t.Fatalf("filed meanwhile: open %v, by %q", open, by)
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

// The same for a Feishu item a plugin took over: the new message keeps it open, the retry keeps the derived client
// id and is not sent again; a request with its own client_id is another reply.
func TestMigratedReplyKeepsMessagesThatCameInMeanwhile(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	if err := h.inboxReady(ctx); err != nil {
		t.Fatal(err)
	}
	p := &extensionLark{}
	racing := &extensionLarkRacing{extensionLark: p}
	h.addConnector(racing, true)
	now := time.Now()
	it := &InboxItem{ID: "lark:10:3", Source: "lark", Kind: KindDM, Account: "feishu", AccountName: "飞书", Title: "Alice", Last: "3", Count: 1,
		StartedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), Lark: &InboxLark{AccountID: 1, FeedID: "10", StartPosition: "3"}}
	h.inbox.mu.Lock()
	h.inbox.items[it.ID] = it
	h.inbox.mu.Unlock()
	racing.during = func() { // Alice writes again while the reply is out
		h.inbox.mu.Lock()
		it.Last, it.Count, it.Preview = "5", 2, "还有一件事"
		h.inbox.mu.Unlock()
	}

	if _, err := h.replyInbox(ctx, it.ID, "在的"); err != nil {
		t.Fatal(err)
	}
	if open, _, by := stillOpen(h, it); !open || by != "" || len(p.got) != 1 {
		t.Fatalf("filed with Alice's new message: open %v, by %q, sent %d", open, by, len(p.got))
	}
	if _, err := h.replyInbox(ctx, it.ID, "在的"); err != nil || len(p.got) != 1 {
		t.Fatalf("a retry went out again: %v, sent %d", err, len(p.got))
	}

	// A device that names its sends: another id is another reply, even with the same words.
	const id = "6f1c2b3a-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	if _, err := h.replyInboxAs(ctx, it.ID, "在的", id); err != nil {
		t.Fatal(err)
	}
	if open, _, by := stillOpen(h, it); len(p.got) != 2 || p.got[1].ClientID != id || open || by != "replied" {
		t.Fatalf("a reply of its own: %+v, open %v, by %q", p.got, open, by)
	}
}

// A reply goes out but the row filing the item cannot be written (#4627): the item stays filed in memory (the reply
// went out, a repeat must not send it again), and the same reply sent again writes the row then, so a restart does
// not bring the item back as "Needs you".
func TestReplyRepeatWritesTheRowThatFailed(t *testing.T) {
	ctx := context.Background()
	h, st := newBulkFailHub(t)
	h.in.svc = h.Service
	h.inbox.init()
	c := &inboxConnector{things: []connector.InboxThing{thing("a", time.Now().Add(-time.Minute))}}
	h.addConnector(c, false)
	h.collectInbox(ctx, roundPoll)
	it := openItem(t, h)

	st.fail = map[string]bool{it.ID: true}
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if open, _, by := stillOpen(h, it); open || by != "replied" || st.doneAt(it.ID) != nil {
		t.Fatalf("after a failed write: open %v, by %q, stored done %v", open, by, st.doneAt(it.ID))
	}

	st.fail = nil
	if _, err := h.replyInboxAs(ctx, it.ID, "看过了", ""); err != nil {
		t.Fatal(err)
	}
	if len(c.replies) != 1 {
		t.Fatalf("the repeat went out again: %v", c.replies)
	}
	if st.doneAt(it.ID) == nil {
		t.Fatal("the repeat did not write the item as done: a restart brings it back")
	}
}
