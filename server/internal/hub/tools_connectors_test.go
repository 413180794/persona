package hub

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
)

// The coding agents get mail's tools and not Feishu's (nor can they call them); a mail_send that could not be sent
// is refused before the user is asked.
func TestMailAndFeishuToolsThroughTheirConnectors(t *testing.T) {
	h := newTestHub(t)
	h.mail = (*mail.Service)(nil)
	h.lark = lark.New(lark.Config{BaseURL: "http://127.0.0.1:1", Token: "t"})
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	var agent []string
	for _, tl := range h.mcpTools(false) {
		agent = append(agent, tl.Name)
	}
	if !slices.Contains(agent, "mail_send") || !slices.Contains(agent, "mail_read") || slices.Contains(agent, "lark_chats") {
		t.Fatalf("agent tools %v", agent)
	}

	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	mcpCall := func(name, args string, assistant bool) proto.ToolResult {
		params, _ := json.Marshal(map[string]any{"name": name, "args": json.RawMessage(args), "assistant": assistant})
		out, err := m.onRequest(context.Background(), "mcp.call", params)
		if err != nil {
			t.Fatal(err)
		}
		return out.(proto.ToolResult)
	}
	if r := mcpCall("lark_chats", `{}`, false); !r.IsError || !strings.Contains(r.Content[0].Text, "unknown tool") {
		t.Fatalf("an agent called lark_chats: %+v", r)
	}
	if r := mcpCall("mail_send", `{"to":"not a list"}`, false); !r.IsError || !strings.Contains(r.Content[0].Text, "invalid arguments") {
		t.Fatalf("invalid mail_send: %+v", r)
	}
	if n := len(h.pendingInteractions()); n != 0 {
		t.Fatalf("the user was asked about a mail that could not be sent (%d cards)", n)
	}
}

// mail_send through the mail connector asks with the card it always had (tool, summary, arguments), and a declined
// mail is not sent.
func TestMailSendAsksWithTheSameCard(t *testing.T) {
	h := newTestHub(t)
	m := &replyMail{}
	h.mail = m
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	args := json.RawMessage(`{"account_id":"acc1","to":[{"email":"someone@example.com"}],"subject":"季度汇报","text":"附件已更新"}`)
	done := make(chan proto.ToolResult, 1)
	go func() { done <- h.callConnector(context.Background(), "box", "mail_send", args) }()
	var ix []proto.Interaction
	for i := 0; i < 500 && len(ix) == 0; i++ {
		time.Sleep(5 * time.Millisecond)
		ix = h.pendingInteractions()
	}
	if len(ix) != 1 || ix[0].ToolName != "mail_send" || ix[0].Summary != "Send mail to someone@example.com: 季度汇报" || string(ix[0].ToolInput) != string(args) {
		t.Fatalf("card %+v", ix)
	}
	deny := false
	if err := h.resolve(ix[0].ID, proto.Resolution{Allow: &deny}); err != nil {
		t.Fatal(err)
	}
	if r := <-done; !r.IsError || len(m.sent) != 0 {
		t.Fatalf("declined but sent: %+v %v", r, m.sent)
	}
}
