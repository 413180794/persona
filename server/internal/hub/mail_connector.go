package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// mailConnector is mail as a connector (docs/plan-connectors.md「三、迁移」): so far what goes into the knowledge
// base, by one cursor per account (the newest UID archived), its inbox, its tools and mail_send through the gate;
// 设置 › 连接器 draws its row from the connectors list (?all=1, migration M3–M6).
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

// Tools are mail's (prompts/mcp/mail) without mail_send, which is an action: the user allows each mail on the phone
// (the gate), the card as it always was.
func (c mailConnector) Tools() []proto.Tool {
	var out []proto.Tool
	for _, t := range c.h.mail.Tools() {
		if t.Name != mail.ToolSend {
			out = append(out, t)
		}
	}
	return out
}

func (c mailConnector) Actions() []proto.Tool {
	for _, t := range c.h.mail.Tools() {
		if t.Name == mail.ToolSend {
			return []proto.Tool{t}
		}
	}
	return nil
}

// ForAgents: the coding agents get mail too.
func (mailConnector) ForAgents() bool { return true }

func (c mailConnector) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return toolText(c.h.mail.Call(ctx, name, args, nil))
}

// Validate refuses a mail_send that could not be sent before the user is asked.
func (mailConnector) Validate(name string, args json.RawMessage) error {
	if name != mail.ToolSend {
		return nil
	}
	_, err := mail.SendSummary(args)
	return err
}

func (mailConnector) Describe(_ context.Context, name string, args json.RawMessage) (connector.Action, string) {
	summary, _ := mail.SendSummary(args)
	return connector.Action{Name: name}, summary
}

// Do sends it (the gate let it through).
func (c mailConnector) Do(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return toolText(c.h.mail.Call(ctx, name, args, nil))
}

// toolText is a tool result as text, its error as an error (connector.ReadTools / Actions return them that way).
func toolText(r proto.ToolResult) (string, error) {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	if r.IsError {
		return "", errors.New(b.String())
	}
	return b.String(), nil
}
