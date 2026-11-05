package assistant

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// Replies go to the connector an item belongs to (the hub sends them): mail's to mail (only document items move a status), an item
// nobody owns cannot be answered, a connector gets the move it answers.
func TestInboxRepliesGoToTheirConnector(t *testing.T) {
	ctx := context.Background()
	h := newInboxHub(t)
	h.inboxReady(ctx)
	m := &fakeMail{}
	h.in.mail = m
	rc := &replierConnector{}
	h.addConnector(rc, false)
	put := func(it *InboxItem) {
		it.UpdatedAt = time.Now()
		h.inbox.mu.Lock()
		h.inbox.items[it.ID] = it
		h.inbox.mu.Unlock()
	}
	put(&InboxItem{ID: "mail:acc1:t:9", Source: "mail", Kind: KindMail, Mail: &InboxMail{AccountID: "acc1", Mailbox: "INBOX", UIDs: []uint32{7, 9}}})
	put(&InboxItem{ID: "orphan", Kind: KindMail})
	put(&InboxItem{ID: "example:1", Source: "example", Last: "m2", Connector: &InboxConnector{Thread: "example-org/example-repo#12", CanReply: true}})

	if _, err := h.replyInbox(ctx, "mail:acc1:t:9", "收到", DocChange{Ref: "r", Field: "f", Value: "v"}); err == nil {
		t.Fatal("a mail item moved a status")
	}
	out, err := h.replyInbox(ctx, "mail:acc1:t:9", "收到")
	if err != nil || out.DoneBy != "replied" || len(m.sent) != 1 || m.sent[0]["account_id"] != "acc1" || m.sent[0]["uid"] != float64(9) ||
		m.sent[0]["mailbox"] != "INBOX" || m.sent[0]["text"] != "收到" {
		t.Fatalf("mail reply: %v %+v %+v", err, out, m.sent)
	}
	if _, err := h.replyInbox(ctx, "orphan", "x"); err != errCannotReply {
		t.Fatalf("orphan: %v", err)
	}
	if _, err := h.replyInbox(ctx, "example:1", "看过了"); err != nil || len(rc.got) != 1 ||
		!reflect.DeepEqual(rc.got[0], connector.InboxReply{Thread: "example-org/example-repo#12", ItemID: "example:1", Last: "m2", Text: "看过了"}) {
		t.Fatalf("ReplyItem: %v %+v", err, rc.got)
	}
}
