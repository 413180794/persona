package hub

import (
	"context"
	"errors"

	"github.com/chyroc/jarvis/server/internal/lark"
)

// The inbox of the hub's own connectors, Feishu and mail (docs/plan-connectors.md M3): their items keep their own
// blocks (lark, doc, moment, mail), which the apps and the stored items rely on, so they fill the inbox and answer
// its items the way they always did instead of through connector.Inbox's generic things. collectInbox and
// replyInbox find them in the registry like any connector.

// ownInbox is a connector of the hub's own with inbox items.
type ownInbox interface {
	// collectInbox reads what is new; full also reads the sources polled slowly (mail: every inboxMailPoll, and on
	// a refresh).
	collectInbox(ctx context.Context, full bool) []*InboxItem
	// replyInbox sends the user's reply to one of its items; already: the same reply went out before (nothing sent,
	// the item stays as it is).
	replyInbox(ctx context.Context, r inboxReplyReq) (already bool, err error)
}

// inboxReplyReq is one reply: the item as it is in the inbox (live, under h.inbox.mu) and a copy taken for the
// reply, the text, the documents' status moves, and the item's replies (held for the whole reply).
type inboxReplyReq struct {
	id      string
	live    *InboxItem
	item    InboxItem
	text    string
	changes []DocChange
	once    *inboxReply
}

// ownInboxFor is the connector an item of the hub's own belongs to, by its block (older stored items may have no
// source): Feishu's chats, documents and removed, or mail.
func (h *Hub) ownInboxFor(it InboxItem) ownInbox {
	id := ""
	switch {
	case it.Lark != nil || it.Doc != nil || it.Moment != nil:
		id = "lark"
	case it.Mail != nil:
		id = "mail"
	}
	own, _ := h.connectors.Get(id).(ownInbox)
	return own
}

func (c larkConnector) collectInbox(ctx context.Context, _ bool) []*InboxItem {
	h := c.h
	var changed []*InboxItem
	ch, err := h.collectLark(ctx)
	if err != nil {
		h.logf("inbox: feishu: %v", err)
	}
	changed = append(changed, ch...)
	docs, err := h.collectDocs(ctx)
	if err != nil {
		h.logf("inbox: feishu docs: %v", err)
	}
	changed = append(changed, docs...)
	moments, err := h.collectMoments(ctx)
	if err != nil {
		h.logf("inbox: removed: %v", err)
	}
	return append(changed, moments...)
}

// replyInbox: a removed comment, a document's status moves then its comment, or the Feishu chat's message (sent once:
// the same text on an item already replied to went out already).
func (c larkConnector) replyInbox(ctx context.Context, r inboxReplyReq) (bool, error) {
	h, it := c.h, r.item
	switch {
	case it.Moment != nil && len(r.changes) == 0:
		return false, h.replyMoment(ctx, *it.Moment, r.text)
	case it.Doc != nil:
		// The status moves first: a reply saying "done" with the row still 进行中 would be wrong.
		for _, ch := range r.changes {
			if err := h.writeDocStatus(ctx, *it.Doc, ch); err != nil {
				return false, err
			}
		}
		if r.text != "" {
			return false, h.replyDoc(ctx, *it.Doc, r.text)
		}
		return false, nil
	case len(r.changes) > 0:
		return false, errors.New("only document items have a status to change")
	case it.Lark != nil:
		// The id only depends on the item and the text, so lark-connector drops a repeat even after a hub restart.
		clientID := lark.NamedClientID(r.id + "\n" + r.text)
		h.inbox.mu.Lock()
		sent := r.live.DoneBy == "replied" && r.once.clientID == clientID
		h.inbox.mu.Unlock()
		if sent {
			return true, nil
		}
		// Different text on an item already replied to is a second message the user means to send.
		if err := h.sendLarkReply(ctx, it, r.text, clientID); err != nil {
			return false, err
		}
		r.once.clientID = clientID
		return false, nil
	}
	return false, errCannotReply
}

// collectInbox: mail only on a full round (IMAP is slow; Gmail goes through a proxy).
func (c mailConnector) collectInbox(ctx context.Context, full bool) []*InboxItem {
	if !full {
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
