package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Tool names exposed to Claude through MCP. The hub intercepts anything with a
// "mail_" prefix and routes it to Call.
const (
	ToolAccounts = "mail_accounts"
	ToolSearch   = "mail_search"
	ToolRead     = "mail_read"
	ToolSend     = "mail_send"
)

// Tools returns the MCP tool definitions for mail.
func (s *Service) Tools() []proto.Tool {
	return []proto.Tool{
		{
			Name:        ToolAccounts,
			Description: "List the mail accounts the user has configured (name, address, provider). Call this before searching or sending mail so you know which account_id to use.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			Name:        ToolSearch,
			Description: "Search mail. Returns message summaries (uid, subject, from, date, snippet). Use account_id from mail_accounts; omit it to search every account's inbox at once. For Gmail the query uses Gmail search syntax (from:, subject:, has:attachment, newer_than:7d); other providers treat it as a full-text search.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"account_id":{"type":"string","description":"Account id from mail_accounts; omit to search all inboxes"},
					"mailbox":{"type":"string","description":"Mailbox name, default INBOX"},
					"query":{"type":"string","description":"Search text or Gmail query"},
					"limit":{"type":"integer","description":"Max results, default 20, max 200"},
					"before_uid":{"type":"integer","description":"Return messages with UID lower than this, for paging"}
				},
				"additionalProperties":false
			}`),
		},
		{
			Name:        ToolRead,
			Description: "Read one message in full: headers, plain text, HTML and the attachment list. Does not mark the message as read.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"account_id":{"type":"string"},
					"uid":{"type":"integer"},
					"mailbox":{"type":"string","description":"Mailbox name, default INBOX"}
				},
				"required":["account_id","uid"],
				"additionalProperties":false
			}`),
		},
		{
			Name:        ToolSend,
			Description: "Send an email from one of the user's accounts. The user must approve each send on their phone before it goes out, so fill in a clear subject and body. To reply, pass in_reply_to with the original Message-ID and references with the original References chain plus that Message-ID.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"account_id":{"type":"string"},
					"to":{"type":"array","items":{"$ref":"#/$defs/address"}},
					"cc":{"type":"array","items":{"$ref":"#/$defs/address"}},
					"bcc":{"type":"array","items":{"$ref":"#/$defs/address"}},
					"subject":{"type":"string"},
					"text":{"type":"string","description":"Plain-text body"},
					"html":{"type":"string","description":"Optional HTML body"},
					"in_reply_to":{"type":"string","description":"Message-ID being replied to"},
					"references":{"type":"string","description":"Space-separated Message-IDs from the original thread"}
				},
				"required":["account_id","to","subject","text"],
				"$defs":{
					"address":{"type":"object","properties":{"name":{"type":"string"},"email":{"type":"string"}},"required":["email"]}
				},
				"additionalProperties":false
			}`),
		},
	}
}

// Call dispatches one MCP tool invocation. approve is called before sending a
// mail and must return false if the user declined.
func (s *Service) Call(ctx context.Context, name string, args json.RawMessage, approve func(ctx context.Context, summary string, input json.RawMessage) bool) proto.ToolResult {
	switch name {
	case ToolAccounts:
		accounts, err := s.ListAccounts()
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		if len(accounts) == 0 {
			return proto.TextResult("No mail accounts are configured. Ask the user to add one in the Jarvis app.", true)
		}
		var b strings.Builder
		for _, a := range accounts {
			fmt.Fprintf(&b, "- %s (%s), provider %s, address %s\n", a.Name, a.ID, a.Provider, a.Email)
		}
		return proto.TextResult(strings.TrimSpace(b.String()), false)

	case ToolSearch:
		var req struct {
			AccountID string `json:"account_id"`
			Mailbox   string `json:"mailbox"`
			Query     string `json:"query"`
			Limit     int    `json:"limit"`
			BeforeUID uint32 `json:"before_uid"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return proto.TextResult("invalid arguments: "+err.Error(), true)
		}
		if req.Limit == 0 {
			req.Limit = 20
		}
		res, err := s.ListMessages(ctx, ListOptions{
			AccountID: req.AccountID, Mailbox: req.Mailbox, Query: req.Query, Limit: req.Limit, BeforeUID: req.BeforeUID,
		})
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		if len(res.Messages) == 0 {
			return proto.TextResult("No messages matched.", false)
		}
		var b strings.Builder
		for _, m := range res.Messages {
			fmt.Fprintf(&b, "- uid=%d account=%s mailbox=%s date=%s from=%s <%s> subject=%q\n  %s\n",
				m.UID, m.AccountID, m.Mailbox, m.Date, m.From.Name, m.From.Email, m.Subject, m.Snippet)
		}
		if res.HasMore {
			b.WriteString("(more results available; pass before_uid=")
			fmt.Fprintf(&b, "%d to continue)\n", res.Messages[len(res.Messages)-1].UID)
		}
		return proto.TextResult(b.String(), false)

	case ToolRead:
		var req struct {
			AccountID string `json:"account_id"`
			UID       uint32 `json:"uid"`
			Mailbox   string `json:"mailbox"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return proto.TextResult("invalid arguments: "+err.Error(), true)
		}
		msg, err := s.GetMessage(ctx, req.AccountID, req.UID, req.Mailbox)
		if err != nil {
			return proto.TextResult(err.Error(), true)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "From: %s <%s>\n", msg.From.Name, msg.From.Email)
		fmt.Fprintf(&b, "To: %s\n", formatAddresses(msg.To))
		if len(msg.Cc) > 0 {
			fmt.Fprintf(&b, "Cc: %s\n", formatAddresses(msg.Cc))
		}
		fmt.Fprintf(&b, "Date: %s\nSubject: %s\nMessage-ID: <%s>\n", msg.Date, msg.Subject, msg.MessageID)
		if msg.InReplyTo != "" {
			fmt.Fprintf(&b, "In-Reply-To: <%s>\n", msg.InReplyTo)
		}
		if len(msg.References) > 0 {
			fmt.Fprintf(&b, "References: %s\n", strings.Join(wrapIDs(msg.References), " "))
		}
		if len(msg.Attachments) > 0 {
			b.WriteString("Attachments:\n")
			for _, att := range msg.Attachments {
				fmt.Fprintf(&b, "- %s (%s, %d bytes)\n", att.Filename, att.Mime, att.Size)
			}
		}
		b.WriteString("\n")
		body := msg.Text
		if strings.TrimSpace(body) == "" {
			body = stripHTML(msg.HTML)
		}
		b.WriteString(strings.TrimSpace(body))
		if msg.HTML != "" && msg.Text != "" {
			b.WriteString("\n\n[An HTML version of this message also exists.]")
		}
		return proto.TextResult(b.String(), false)

	case ToolSend:
		var req SendRequest
		if err := json.Unmarshal(args, &req); err != nil {
			return proto.TextResult("invalid arguments: "+err.Error(), true)
		}
		summary := fmt.Sprintf("Send mail to %s: %s", formatAddresses(req.To), req.Subject)
		if approve != nil && !approve(ctx, summary, args) {
			return proto.TextResult("The user declined to send this email.", true)
		}
		if err := s.Send(ctx, req); err != nil {
			return proto.TextResult(err.Error(), true)
		}
		return proto.TextResult("Mail sent to "+formatAddresses(req.To)+".", false)
	}
	return proto.TextResult("unknown tool "+name, true)
}

func formatAddresses(addrs []Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Email))
		} else {
			parts = append(parts, a.Email)
		}
	}
	return strings.Join(parts, ", ")
}

func wrapIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "<" + id + ">"
	}
	return out
}
