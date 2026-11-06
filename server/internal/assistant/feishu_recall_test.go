package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

// A message the owner recalled before it went into its conversation is not sent, and its OnIt reaction goes.
func TestFeishuRecalledMessageIsNotSent(t *testing.T) {
	f, _ := feishuHub("ou_a", time.Unix(100, 0))
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	f.setSessions("server", proto.Session{SessionID: "m1", Assistant: true})
	unreacted := 0
	f.answerApp(AppFeishu, FeishuReact, func(json.RawMessage) (any, error) { return map[string]any{"reaction_id": "r1"}, nil })
	f.answerApp(AppFeishu, FeishuUnreact, func(json.RawMessage) (any, error) { unreacted++; return map[string]any{}, nil })
	s := newTestService(t, f)
	var ev feishubot.MessageEvent
	ev.Message.MessageID, ev.Message.ChatType, ev.Message.MessageType, ev.Message.Content = "om_x", "p2p", "text", `{"text":"发错了"}`

	s.FeishuRecalled(FeishuRecalled{MessageID: "om_x"})
	s.feishuMessage(ev, FeishuStatus{Ready: true, Features: FeishuFeatures{Main: true}}, "")
	if text := sendOnceText(f); text != "" {
		t.Fatalf("a recalled message was sent: %q", text)
	}
	// Recalled while it was being read (its downloads): botSend stops before it goes in.
	ev.Message.MessageID = "om_y"
	content := feishubot.ParseContent(ev.Message.MessageType, ev.Message.Content, nil)
	s.FeishuRecalled(FeishuRecalled{MessageID: "om_y"})
	if err := s.botSend(context.Background(), &ev, content, botTarget{Kind: targetMain}); err != nil {
		t.Fatal(err)
	}
	if text := sendOnceText(f); text != "" || unreacted != 1 {
		t.Fatalf("sent %q, reactions taken off %d", text, unreacted)
	}
	// Another message goes as before.
	ev.Message.MessageID = "om_z"
	if err := s.botSend(context.Background(), &ev, content, botTarget{Kind: targetMain}); err != nil || sendOnceText(f) == "" {
		t.Fatalf("an ordinary message: %v", err)
	}
}
