package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
)

func TestOwnerCardChannelChecksPairingAndReceipt(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.h.cfg.Bots.Feishu.OwnerCardChannel = "example/ownerchat"
	card := feishubot.NewCard("Test conclusion", feishubot.Green, "Checked").JSON()
	body := map[string]any{"card": card, "kind": "conclusion", "uuid": "round-1", "fallback": "Checked"}
	var sends []channel.SendParams
	receipt := "om_owner"
	w.h.channels.call = func(_ context.Context, id, method string, params, out any) error {
		if id != "ownerchat" || method != channel.MSend {
			t.Fatalf("unexpected delivery %q %q", id, method)
		}
		sends = append(sends, params.(channel.SendParams))
		out.(*channel.SendResult).MessageID = receipt
		return nil
	}
	w.api.sentBy(receipt, "oc_p2p", "cli_test")
	w.api.mu.Lock()
	w.api.read[receipt].([]map[string]any)[0]["msg_type"] = feishubot.TypeInteractive
	w.api.mu.Unlock()
	result := w.feishu(t, assistantwire.FeishuPost, body)
	if result["message_id"] != receipt || len(sends) != 1 {
		t.Fatalf("receipt %v, sends %d", result, len(sends))
	}
	got := sends[0]
	want := channel.FeishuOwnerCard{Purpose: "conclusion", AppID: "cli_test", OwnerOpenID: "ou_me", Card: card}
	if got.Ref != "round-1" || got.Text != "Checked" || got.Kind != channel.OutPush || got.FeishuCard == nil || *got.FeishuCard != want {
		t.Fatalf("wrong owner card: %+v", got)
	}
	for _, tc := range []struct{ id, chat, sender string }{
		{"om_other_bot", "oc_p2p", "cli_other"},
		{"om_other_chat", "oc_group", "cli_test"},
		{"om_by_user", "oc_p2p", ""},
		{"om_text_only", "oc_p2p", "cli_test"},
		{"", "", ""},
	} {
		receipt = tc.id
		w.api.sentBy(receipt, tc.chat, tc.sender)
		if err := w.feishuErr(assistantwire.FeishuPost, body); err == nil {
			t.Fatalf("accepted receipt from %q/%q/%q", tc.id, tc.chat, tc.sender)
		}
	}
	before := len(sends)
	for _, key := range []string{"reply_to", "chat_id"} {
		body[key] = "elsewhere"
		if err := w.feishuErr(assistantwire.FeishuPost, body); err == nil || len(sends) != before {
			t.Fatalf("out-of-DM target %s reached the transport", key)
		}
		delete(body, key)
	}
	raw, _ := json.Marshal(map[string]any{"channel": "ownerchat", "kind": "push", "feishu_card": want})
	if _, err := w.h.channelsCall(w.ctx, assistantwire.ChannelsSend, raw); err == nil || len(sends) != before {
		t.Fatal("a generic channel call injected a native card")
	}
}

func TestPairedBotSendsAfterReservationAndConfirmsReadback(t *testing.T) {
	w := newBotWorld(t)
	w.pair(t)
	w.api.mu.Lock()
	w.api.echoSentCards = true
	w.api.mu.Unlock()
	w.h.cfg.Bots.Feishu.OwnerCardChannel = "example/ownerchat"
	checked := ""
	checks := 0
	w.h.channels.call = func(_ context.Context, _, _ string, params, out any) error {
		p := params.(channel.SendParams)
		if p.Ref != "review-1" || p.FeishuCard.Purpose != "conclusion" || p.FeishuCard.OwnerOpenID != "ou_me" {
			t.Fatalf("different delivery: %+v", p)
		}
		r := out.(*channel.SendResult)
		if p.FeishuCard.MessageID != "" {
			checks++
			checked = p.FeishuCard.MessageID
		}
		if checked != "" {
			r.MessageID = checked
		} else {
			r.Prepared = true
		}
		return nil
	}
	card := feishubot.NewCard("Test conclusion", feishubot.Green, "Checked").JSON()
	body := map[string]any{"card": card, "kind": "conclusion", "uuid": "review-1", "fallback": "Checked"}
	before := w.api.total()
	for range 2 {
		got := w.feishu(t, assistantwire.FeishuPost, body)
		if checked == "" || got["message_id"] != checked {
			t.Fatalf("readback did not settle in the outbox: %v", got)
		}
	}
	sends := w.api.since(before)
	if checks != 1 || strings.Count(sends, "POST /open-apis/im/v1/messages") != 1 || !strings.Contains(sends, `"receive_id":"oc_p2p"`) ||
		!strings.Contains(sends, `"msg_type":"interactive"`) || !strings.Contains(sends, `"uuid":`) {
		t.Fatalf("wanted one paired send and confirmation: %v (%d)", sends, checks)
	}
}
