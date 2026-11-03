package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/kb"
)

// The knowledge base's reading (connector.Archive): one cursor for the account, which holds, for each conversation,
// the newest message it archived. Every round lists the conversations and reads the ones with something newer; a
// conversation read the first time goes back firstDays, at most firstMax messages (the user's choice, 2026-11-03).
var (
	firstDays    = 30 * 24 * time.Hour
	firstMax     = 500
	dialogsLimit = 200             // conversations listed per round, per folder: newest first, so what moved is at the top
	newsSettle   = 3 * time.Second // Telegram's updates come in bursts: one kick for each
)

type archiveCursor struct {
	Seen map[string]int `json:"seen"` // peer key -> the newest message archived
}

// client is the connected client, or nil.
func (c *Conn) client() client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cl
}

var errNotConnected = errors.New("not connected to Telegram")

// Changes gives the conversations with messages newer than the cursor, at most limit of them. The gate paces every
// request (gate.go). A Telegram error part way ends the round: what was read (and the cursor up to it) goes to the
// hub, the rest waits for the next round; during a flood wait it asks nothing.
func (c *Conn) Changes(ctx context.Context, cursor string, limit int) ([]connector.ChatBatch, string, bool, error) {
	cl := c.client()
	if cl == nil {
		return nil, cursor, false, errNotConnected
	}
	if left := c.gate.left(); left > 0 {
		return nil, cursor, false, heldError{left}
	}
	var cur archiveCursor
	if cursor != "" {
		if err := json.Unmarshal([]byte(cursor), &cur); err != nil {
			return nil, cursor, false, fmt.Errorf("the archive's cursor does not read: %w", err)
		}
	}
	if cur.Seen == nil {
		cur.Seen = map[string]int{}
	}
	ds, err := cl.Dialogs(ctx, dialogsLimit)
	if err != nil {
		return nil, cursor, false, scrubbed(err)
	}
	c.noteDialogs(ds) // the inbox reads the same list
	var batches []connector.ChatBatch
	more := false
	for _, d := range ds {
		key := d.Peer.key()
		if !d.kept() || d.Top <= cur.Seen[key] {
			continue
		}
		if len(batches) >= limit {
			more = true
			break
		}
		seen := cur.Seen[key]
		var since time.Time
		if seen == 0 {
			since = time.Now().Add(-firstDays)
		}
		msgs, err := cl.History(ctx, d, seen, since, firstMax)
		if err != nil {
			log.Printf("archive: %s: %s", key, explain(err))
			if len(batches) == 0 {
				return nil, cursor, false, scrubbed(err)
			}
			break // keep what was read; the rest next round
		}
		top := seen
		for _, m := range msgs {
			top = max(top, m.ID)
		}
		cur.Seen[key] = max(top, d.Top)
		if len(msgs) > 0 {
			batches = append(batches, connector.ChatBatch{Chat: kb.SourceChat{ID: key, Name: d.Name, Kind: d.Kind, UpdatedAt: d.TopAt},
				Messages: sourceMessages(msgs)})
		}
		if ctx.Err() != nil {
			return batches, encodeCursor(cur), true, nil
		}
	}
	c.mu.Lock()
	c.status.LastSync = time.Now()
	c.mu.Unlock()
	return batches, encodeCursor(cur), more, nil
}

func encodeCursor(cur archiveCursor) string {
	b, _ := json.Marshal(cur)
	return string(b)
}

// sourceMessages turns messages into the knowledge base's, a reply's words naming what it answers when that is in
// the same batch.
func sourceMessages(msgs []message) []kb.SourceMessage {
	text := map[int]string{}
	for _, m := range msgs {
		text[m.ID] = m.Text
	}
	out := make([]kb.SourceMessage, 0, len(msgs))
	for _, m := range msgs {
		t := m.Text
		if m.ReplyTo != 0 {
			if quoted, ok := text[m.ReplyTo]; ok && quoted != "" {
				t = "回复「" + clip(quoted, 30) + "」：" + t
			} else {
				t = "回复一条消息：" + t
			}
		}
		sm := kb.SourceMessage{ID: strconv.Itoa(m.ID), SenderID: m.FromKey, SenderName: m.From, Mine: m.Out, Type: m.Type, Text: t, At: m.At}
		if m.FromKey != "" {
			sm.SenderHandles = []kb.Handle{{Kind: kb.HandleTelegram, Value: m.FromKey}}
		}
		out = append(out, sm)
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// updated is Telegram telling of something new: Watch kicks the hub once the burst settles.
func (c *Conn) updated() {
	select {
	case c.news <- struct{}{}:
	default:
	}
}

// Watch kicks the hub's archive and inbox when Telegram tells of something new (connector.Events).
func (c *Conn) Watch(ctx context.Context, changed func(connector.Scope)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.news:
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(newsSettle):
		}
		select { // what came while it settled is in this kick
		case <-c.news:
		default:
		}
		changed(connector.ScopeChats)
		c.refreshInbox(ctx) // the inbox answers from memory: bring it up to date first
		changed(connector.ScopeInbox)
	}
}
