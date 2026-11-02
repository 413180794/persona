package hub

import (
	"context"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// replyMail records what it was asked to send.
type replyMail struct {
	Mail
	sent []mail.SendRequest
}

func (*replyMail) GetMessage(_ context.Context, account string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	return &mail.MailMessage{MailSummary: mail.MailSummary{AccountID: account, UID: uid, Mailbox: mailbox, Subject: "季度汇报",
		MessageID: "<m9@example.com>", From: mail.Address{Email: "sender@example.com"}}, Text: "请周五前提交"}, nil
}

func (m *replyMail) Send(_ context.Context, r mail.SendRequest) error {
	m.sent = append(m.sent, r)
	return nil
}

// replierConnector answers with ReplyItem and records it.
type replierConnector struct{ got []connector.InboxReply }

func (*replierConnector) ID() string                                              { return "example" }
func (*replierConnector) Name() string                                            { return "Example" }
func (*replierConnector) Collect(context.Context) ([]connector.InboxThing, error) { return nil, nil }
func (*replierConnector) Reply(context.Context, string, string) error {
	panic("ReplyItem should be used")
}
func (c *replierConnector) ReplyItem(_ context.Context, r connector.InboxReply) error {
	c.got = append(c.got, r)
	return nil
}

// Replies go to the connector an item belongs to: mail's to mail (only document items move a status), an item
// nobody owns cannot be answered, a connector with ReplyItem gets the move it answers.
func TestInboxRepliesGoToTheirConnector(t *testing.T) {
	ctx := context.Background()
	h := newTestHub(t)
	h.inbox.init()
	m := &replyMail{}
	h.mail = m
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	rc := &replierConnector{}
	if err := h.addConnector(rc); err != nil {
		t.Fatal(err)
	}
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
	if err != nil || out.DoneBy != "replied" || len(m.sent) != 1 || m.sent[0].AccountID != "acc1" || m.sent[0].InReplyTo != "<m9@example.com>" {
		t.Fatalf("mail reply: %v %+v %+v", err, out, m.sent)
	}
	if _, err := h.replyInbox(ctx, "orphan", "x"); err != errCannotReply {
		t.Fatalf("orphan: %v", err)
	}
	if _, err := h.replyInbox(ctx, "example:1", "看过了"); err != nil || len(rc.got) != 1 ||
		rc.got[0] != (connector.InboxReply{Thread: "example-org/example-repo#12", ItemID: "example:1", Last: "m2", Text: "看过了"}) {
		t.Fatalf("ReplyItem: %v %+v", err, rc.got)
	}
}

func (*replyMail) Tools() []proto.Tool { return (*mail.Service)(nil).Tools() }
