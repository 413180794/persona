package hub

import (
	"context"
	"errors"

)

// The inbox of the hub's own connector with items of its own, mail (docs/plan-connectors.md M3): its items keep their
// own block (mail), which the apps and the stored items rely on, so it fills the inbox and answers its items the way
// it always did instead of through connector.Inbox's generic things. collectInbox and replyInbox find it in the
// registry like any connector. (Feishu's chats, documents and company-feed items come from a plugin's connector now,
// with their blocks in the things' extra: inbox_migrate.go.)

// ownInbox is a connector of the hub's own with inbox items.
type ownInbox interface {
	// collectInbox reads what is new in the sources the round reads (inboxRound).
	collectInbox(ctx context.Context, round inboxRound) []*InboxItem
	// replyInbox sends the user's reply to one of its items; already: the same reply went out before (nothing sent,
	// the item stays as it is).
	replyInbox(ctx context.Context, r inboxReplyReq) (already bool, err error)
}

// inboxReplyReq is one reply: the item as it is in the inbox (live, under h.inbox.mu) and a copy taken for the
// reply, the text, the documents' status moves, and the item's replies (held for the whole reply).
type inboxReplyReq struct {
	id   string
	live *InboxItem
	item InboxItem
	text string
	// clientID: the app's id for the send (POST …/reply client_id); empty for one derived from the item and the text.
	clientID string
	changes  []DocChange
	once     *inboxReply
}

// ownInboxFor is the connector an item of the hub's own belongs to, by its block (older stored items may have no
// source): mail.
func (h *Hub) ownInboxFor(it InboxItem) ownInbox {
	id := ""
	if it.Mail != nil {
		id = "mail"
	}
	own, _ := h.connectors.Get(id).(ownInbox)
	return own
}

// collectInbox: mail only on a full round (IMAP is slow; Gmail goes through a proxy).
func (c mailConnector) collectInbox(ctx context.Context, round inboxRound) []*InboxItem {
	if round != roundFull {
		return nil
	}
	return c.h.collectMail(ctx)
}

// replyInbox answers the newest mail of the thread from the account that received it.
func (c mailConnector) replyInbox(ctx context.Context, r inboxReplyReq) (bool, error) {
	h, m := c.h, r.item.Mail
	if len(r.changes) > 0 {
		return false, errors.New("only document items have a status to change")
	}
	if h.mail == nil || m == nil || len(m.UIDs) == 0 {
		return false, errCannotReply
	}
	orig, err := h.mail.GetMessage(ctx, m.AccountID, m.UIDs[len(m.UIDs)-1], m.Mailbox)
	if err != nil {
		return false, err
	}
	return false, h.mail.Send(ctx, mailReply(m.AccountID, orig, r.text))
}
