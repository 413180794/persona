package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/mail"
)

// mailConnector is mail as a connector (docs/plan-connectors.md「三、迁移」): so far what goes into the knowledge
// base, by one cursor per account (the newest UID archived). The inbox, the tools, the gate and 设置 › 连接器's row
// move to it step by step (M3–M6).
type mailConnector struct{ h *Hub }

func (mailConnector) ID() string   { return "mail" }
func (mailConnector) Name() string { return "邮件" }

// NextPoll: IMAP is slow and Gmail goes through a proxy.
func (mailConnector) NextPoll() time.Duration { return kbMailPoll }

// Changes is inbox mail newer than the last UID archived per account, oldest first, one chat per account and
// correspondent (mail-<account>-<address>); at most 10 full texts a run, the rest their snippet; subject + text,
// 4 000 characters. The cursor is {"<account>": uid}, as mail-cursors.json always was. An account that cannot be
// read is skipped this time.
func (c mailConnector) Changes(ctx context.Context, cursor string, _ int) ([]connector.ChatBatch, string, bool, error) {
	cursors := map[string]uint32{}
	if cursor != "" {
		_ = json.Unmarshal([]byte(cursor), &cursors)
	}
	accounts, err := c.h.mail.ListAccounts()
	if err != nil {
		return nil, cursor, false, nil
	}
	var batches []connector.ChatBatch
	reads := 0
	for _, a := range accounts {
		res, err := c.h.mail.ListMessages(ctx, mail.ListOptions{AccountID: a.ID, Limit: 40})
		if err != nil {
			continue
		}
		last := cursors[a.ID]
		top := last
		for i := len(res.Messages) - 1; i >= 0; i-- { // oldest first
			m := res.Messages[i]
			if m.UID <= last {
				continue
			}
			if m.UID > top {
				top = m.UID
			}
			text := m.Snippet
			if reads < 10 {
				if full, err := c.h.mail.GetMessage(ctx, a.ID, m.UID, m.Mailbox); err == nil && strings.TrimSpace(full.Text) != "" {
					text = full.Text
				}
				reads++
			}
			at, _ := time.Parse(time.RFC3339, m.Date)
			addr := strings.ToLower(m.From.Email)
			name := m.From.Name
			if name == "" {
				name = addr
			}
			batches = append(batches, connector.ChatBatch{
				Chat: kb.SourceChat{ID: a.ID + "-" + addr, Name: name + " · 邮件", Kind: "mail"},
				Messages: []kb.SourceMessage{{ID: fmt.Sprint(m.UID), SenderID: addr, SenderName: name, At: at, Type: "TEXT",
					Mine: strings.EqualFold(addr, a.Email),
					Text: "【" + m.Subject + "】" + clipRunes(strings.TrimSpace(text), 4000)}},
			})
		}
		cursors[a.ID] = top
	}
	raw, _ := json.Marshal(cursors)
	return batches, string(raw), false, nil
}
