package hub

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
)

// The inbox of a connector a plugin took over from the hub (connectors_migrate.go): its things carry the items' own
// blocks (connector.ThingExtra), so the items keep the shape the apps know (Feishu's lark / doc / moment block, no
// generic connector block), and the items the hub made itself before the move go on as the same items: a thread is
// found by the block (migratedThread), the same key the plugin uses.

// migratedThread is the thread of a Feishu item by its block: "chat:<feed>" ("chat:<account>:<feed>" for an account
// other than the primary one), "doc:<notice>", "moment:<post>"; "" for anything else.
func migratedThread(it *InboxItem) string {
	switch {
	case it.Lark != nil:
		if it.Lark.AccountID > 1 {
			return fmt.Sprintf("chat:%d:%s", it.Lark.AccountID, it.Lark.FeedID)
		}
		return "chat:" + it.Lark.FeedID
	case it.Doc != nil:
		return "doc:" + it.Doc.NoticeID
	case it.Moment != nil:
		return "moment:" + it.Moment.PostID
	}
	return ""
}

// applyMigrated is applyConnector for the things with Extra of a migrated connector, under b.mu.
func (b *inbox) applyMigrated(id string, things []connector.InboxThing, now time.Time) []*InboxItem {
	var changed []*InboxItem
	for _, t := range things {
		source := id
		if t.Extra.Source != "" {
			source = t.Extra.Source
		}
		open, done := b.openFor(func(it *InboxItem) bool {
			return it.Source == source && it.Connector == nil && migratedThread(it) == t.Thread
		})
		if t.Done != "" {
			if open != nil {
				open.done(now, t.Done)
				if t.Why != "" {
					open.Reason = t.Why
				}
				changed = append(changed, open)
			}
			continue
		}
		if open != nil {
			switch {
			case open.Last != t.Last:
				fillMigrated(open, t)
				open.Read = false
				changed = append(changed, open)
			case blocksDiffer(open, t.Extra):
				// The same move, read further (a chat's read position) or read again (a document's rows): blocks only.
				setBlocks(open, t.Extra)
				changed = append(changed, open)
			}
			if t.Read && !open.Read {
				open.Read, open.Count = true, t.Extra.Count
				if open.Tier != "" && open.Tier != TierAction { // untriaged ones are settled by triage
					open.done(now, "read")
				}
				if !containsItem(changed, open) {
					changed = append(changed, open)
				}
			}
			continue
		}
		if t.Read || now.Sub(t.At) > inboxWindow || done != nil && (done.Last == t.Last || !t.At.After(done.UpdatedAt)) {
			continue
		}
		itemID := t.Extra.LegacyID
		if _, taken := b.items[itemID]; itemID == "" || taken {
			sum := sha1.Sum([]byte(t.Thread + "\n" + t.Last))
			itemID = id + ":" + hex.EncodeToString(sum[:8])
		}
		it := &InboxItem{ID: itemID, Source: source, Account: t.Extra.Account, AccountName: t.Extra.AccountName, StartedAt: t.Extra.StartedAt}
		if it.StartedAt.IsZero() {
			it.StartedAt = t.At
		}
		fillMigrated(it, t)
		b.items[it.ID] = it
		changed = append(changed, it)
	}
	return changed
}

// fillMigrated sets an item from a thing with Extra; an open chat item keeps where it started.
func fillMigrated(it *InboxItem, t connector.InboxThing) {
	it.Kind, it.Title, it.Sender, it.Preview, it.Context = t.Kind, t.Title, t.Sender, t.Preview, t.Context
	it.UpdatedAt, it.Last = t.At, t.Last
	it.Avatar, it.Count = t.Extra.Avatar, max(t.Extra.Count, 1)
	setBlocks(it, t.Extra)
}

func setBlocks(it *InboxItem, x *connector.ThingExtra) {
	start := ""
	if it.Lark != nil {
		start = it.Lark.StartPosition
	}
	it.Lark, it.Doc, it.Moment = nil, nil, nil
	if len(x.Lark) > 0 && json.Unmarshal(x.Lark, &it.Lark) == nil && it.Lark != nil && start != "" {
		it.Lark.StartPosition = start
	}
	if len(x.Doc) > 0 {
		_ = json.Unmarshal(x.Doc, &it.Doc)
	}
	if len(x.Moment) > 0 {
		_ = json.Unmarshal(x.Moment, &it.Moment)
	}
}

// blocksDiffer: the thing's block says something else than the item's (a chat's start position aside: the item keeps
// its own).
func blocksDiffer(it *InboxItem, x *connector.ThingExtra) bool {
	c := *it
	setBlocks(&c, x)
	enc := func(it *InboxItem) []byte {
		b, _ := json.Marshal([]any{it.Lark, it.Doc, it.Moment})
		return b
	}
	return !bytes.Equal(enc(it), enc(&c))
}

// migratedConnector is the plugin's inbox for a Feishu item, when a plugin took Feishu over (nil otherwise).
func (h *Hub) migratedConnector(it *InboxItem) connector.Inbox {
	if it.Connector != nil || migratedThread(it) == "" || !h.larkByPlugin() {
		return nil
	}
	in, _ := connector.As[connector.Inbox](h.connectors.Get("lark"))
	return in
}

// replyMigrated sends the reply (and a document's status moves) of a Feishu item through the plugin. The client id
// is the one the hub would have used itself, so a retry across the move is still dropped by lark-connector.
func (h *Hub) replyMigrated(ctx context.Context, in connector.Inbox, r inboxReplyReq) (bool, error) {
	it := r.item
	clientID := ""
	if it.Lark != nil {
		clientID = r.clientID
		if !lark.ValidClientID(clientID) {
			clientID = lark.NamedClientID(r.id + "\n" + r.text)
		}
		h.inbox.mu.Lock()
		sent := r.live.DoneBy == "replied" && r.once.clientID == clientID
		h.inbox.mu.Unlock()
		if sent {
			return true, nil
		}
	}
	var changes []connector.StatusChange
	for _, ch := range r.changes {
		changes = append(changes, connector.StatusChange{Ref: ch.Ref, Field: ch.Field, Value: ch.Value})
	}
	if len(changes) > 0 && it.Doc == nil {
		return false, fmt.Errorf("only document items have a status to change")
	}
	if err := in.Reply(ctx, connector.InboxReply{Thread: migratedThread(&it), ItemID: it.ID, Last: it.Last, Text: r.text,
		ClientID: clientID, Changes: changes}); err != nil {
		return false, err
	}
	if clientID != "" {
		r.once.clientID = clientID
	}
	return false, nil
}

// migratedDocStatus moves a document row's status through the plugin; the item's rows come back with its next things.
func (h *Hub) migratedDocStatus(ctx context.Context, in connector.Inbox, it *InboxItem, ch DocChange) (InboxItem, error) {
	h.inbox.mu.Lock()
	thread, itemID, last := migratedThread(it), it.ID, it.Last
	h.inbox.mu.Unlock()
	if err := in.Reply(ctx, connector.InboxReply{Thread: thread, ItemID: itemID, Last: last,
		Changes: []connector.StatusChange{{Ref: ch.Ref, Field: ch.Field, Value: ch.Value}}}); err != nil {
		return InboxItem{}, err
	}
	h.inbox.mu.Lock()
	out := it.listed()
	h.inbox.mu.Unlock()
	return out, nil
}

func isMigrated(id string) bool { return slices.Contains(connector.MigratableConnectors, id) }
