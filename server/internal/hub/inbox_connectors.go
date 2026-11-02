package hub

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
)

// The inbox items of connectors (GitHub…): their things become items with one generic block the apps draw without
// knowing the source (docs/plan-connectors.md「一 · 5. 收件箱」). Feishu and mail keep their own blocks.

// InboxConnector is an item's thread in a connector, and how it is answered.
type InboxConnector struct {
	Thread   string `json:"thread"`
	Label    string `json:"label,omitempty"`    // the row's tag: 请你 review
	URL      string `json:"url,omitempty"`      // the thread on the web, opened in the app
	OpenURL  string `json:"open_url,omitempty"` // a link the system opens (sms:…), not the app
	ReplyTo  string `json:"reply_to,omitempty"` // where a reply goes and who sees it
	CanReply bool   `json:"can_reply,omitempty"`
	// RuleTier is the connector's tier for triage without a model.
	RuleTier string `json:"rule_tier,omitempty"`
}

// collectConnectors applies the things of every signed-in connector with an inbox.
func (h *Hub) collectConnectors(ctx context.Context) []*InboxItem {
	var changed []*InboxItem
	for _, c := range h.connectors.All() {
		in, ok := c.(connector.Inbox)
		if !ok || connector.StatusOf(ctx, c).State != connector.StateOK && connector.StatusOf(ctx, c).State != connector.StateRateLimited {
			continue
		}
		things, err := in.Collect(ctx)
		if err != nil {
			h.logf("inbox: %s: %v", c.ID(), err)
			continue
		}
		h.inbox.mu.Lock()
		changed = append(changed, h.inbox.applyConnector(c.ID(), c.Name(), things, time.Now())...)
		h.inbox.mu.Unlock()
	}
	return changed
}

// applyConnector turns a connector's things into items, under b.mu: one open item per thread gathers its later
// moves until it is done; a move after that starts a new item; a thing that is over closes its open item.
func (b *inbox) applyConnector(id, name string, things []connector.InboxThing, now time.Time) []*InboxItem {
	var changed []*InboxItem
	for _, t := range things {
		open, done := b.openFor(func(it *InboxItem) bool {
			return it.Source == id && it.Connector != nil && it.Connector.Thread == t.Thread
		})
		if t.Done != "" {
			if open != nil {
				open.done(now, t.Done)
				changed = append(changed, open)
			}
			continue
		}
		if open != nil {
			if open.Last != t.Last {
				open.Count++
				fillConnectorItem(open, t)
				changed = append(changed, open)
			}
			if t.Read && !open.Read {
				open.Read = true
				if open.Tier != "" && open.Tier != TierAction { // untriaged ones are settled by triage
					open.done(now, "read")
				}
				if !containsItem(changed, open) {
					changed = append(changed, open)
				}
			}
			continue
		}
		// Nothing open: only a move not yet seen, unread, recent, starts an item.
		if t.Read || now.Sub(t.At) > inboxWindow || done != nil && (done.Last == t.Last || !t.At.After(done.UpdatedAt)) {
			continue
		}
		sum := sha1.Sum([]byte(t.Thread + "\n" + t.Last))
		it := &InboxItem{ID: id + ":" + hex.EncodeToString(sum[:8]), Source: id, Account: id, AccountName: name,
			StartedAt: t.At, Count: 1}
		fillConnectorItem(it, t)
		b.items[it.ID] = it
		changed = append(changed, it)
	}
	return changed
}

func fillConnectorItem(it *InboxItem, t connector.InboxThing) {
	it.Kind, it.Title, it.Sender, it.Preview, it.Context = t.Kind, t.Title, t.Sender, t.Preview, t.Context
	it.UpdatedAt, it.Last = t.At, t.Last
	it.Connector = &InboxConnector{Thread: t.Thread, Label: t.Label, URL: t.URL, OpenURL: t.OpenURL, ReplyTo: t.ReplyTo, CanReply: t.CanReply, RuleTier: t.Tier}
}

func containsItem(list []*InboxItem, it *InboxItem) bool {
	for _, x := range list {
		if x == it {
			return true
		}
	}
	return false
}

// replyConnectorItem sends the user's reply to a connector's item, to its thread through the connector
// (InboxReplier when it has it); the same text on an item already replied to went out already.
func (h *Hub) replyConnectorItem(ctx context.Context, r inboxReplyReq) (already bool, err error) {
	c := r.item
	h.inbox.mu.Lock()
	sent := r.live.DoneBy == "replied" && r.once.clientID == r.text
	h.inbox.mu.Unlock()
	if sent {
		return true, nil
	}
	conn := h.connectors.Get(c.Source)
	in, ok := conn.(connector.Inbox)
	if !ok || !c.Connector.CanReply {
		return false, errCannotReply
	}
	if rp, ok := conn.(connector.InboxReplier); ok {
		err = rp.ReplyItem(ctx, connector.InboxReply{Thread: c.Connector.Thread, ItemID: c.ID, Last: c.Last, Text: r.text})
	} else {
		err = in.Reply(ctx, c.Connector.Thread, r.text)
	}
	if err != nil {
		return false, err
	}
	r.once.clientID = r.text
	return false, nil
}
