package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// An owner action answers only in the channel it was issued in: a card's press that card alone, a message only a
// kind of card the channel takes a typed answer for; once, within its time (#5011).
func TestOwnerActionIsBoundToItsCardAndChannel(t *testing.T) {
	const plugin = "ext-chat"
	press := func(h *Manager) string { return h.issueOwnerAction(channel.Feishu, "ix_card") }
	feishuMessage := func(h *Manager) string { return h.issueMessageAction(channel.Feishu, proto.IxQuestion) }
	pluginMessage := func(h *Manager) string { return h.issueMessageAction(plugin, channel.TypedAnswerKinds...) }
	type take struct{ channel, interaction, kind string }
	cases := []struct {
		name  string
		issue func(*Manager) string
		take  take
		ok    bool
	}{
		{"the press, its card", press, take{channel.Feishu, "ix_card", proto.IxPlan}, true},
		{"the press, another card", press, take{channel.Feishu, "ix_other", proto.IxPlan}, false},
		{"the press, another channel", press, take{plugin, "ix_card", proto.IxPlan}, false},
		{"the press, no channel", press, take{"", "ix_card", proto.IxPlan}, false},
		{"a Feishu message, a question", feishuMessage, take{channel.Feishu, "ix_q", proto.IxQuestion}, true},
		{"a Feishu message, a plan", feishuMessage, take{channel.Feishu, "ix_plan", proto.IxPlan}, false},
		{"a Feishu message, a permission", feishuMessage, take{channel.Feishu, "ix_perm", proto.IxPermission}, false},
		{"a Feishu message, a terminal", feishuMessage, take{channel.Feishu, "ix_screen", proto.IxScreen}, false},
		{"a Feishu message, no kind", feishuMessage, take{channel.Feishu, "ix_q", ""}, false},
		{"a Feishu message, no card", feishuMessage, take{channel.Feishu, "", proto.IxQuestion}, false},
		{"a Feishu message, another channel", feishuMessage, take{plugin, "ix_q", proto.IxQuestion}, false},
		{"a plugin message, a plan", pluginMessage, take{plugin, "ix_plan", proto.IxPlan}, true},
		{"a plugin message, a form", pluginMessage, take{plugin, "ix_form", proto.IxElicitation}, false},
		{"a plugin message, in Feishu", pluginMessage, take{channel.Feishu, "ix_plan", proto.IxPlan}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestManager(t)
			a := c.issue(h)
			err := h.takeOwnerAction(a, c.take.channel, c.take.interaction, c.take.kind)
			if c.ok != (err == nil) || err != nil && !errors.Is(err, errNoOwnerAction) {
				t.Fatalf("take %+v: %v", c.take, err)
			}
			if !c.ok {
				return
			}
			if err := h.takeOwnerAction(a, c.take.channel, c.take.interaction, c.take.kind); !errors.Is(err, errNoOwnerAction) {
				t.Fatalf("used twice: %v", err)
			}
		})
	}
	t.Run("expired", func(t *testing.T) {
		h := newTestManager(t)
		a := press(h)
		in := &h.chans.in
		in.mu.Lock()
		x := in.actions[a]
		x.expires = time.Now().Add(-time.Second)
		in.actions[a] = x
		in.mu.Unlock()
		if err := h.takeOwnerAction(a, channel.Feishu, "ix_card", proto.IxPlan); !errors.Is(err, errNoOwnerAction) {
			t.Fatalf("an expired action: %v", err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		h := newTestManager(t)
		if err := h.takeOwnerAction("oa_000000000000000000000000", channel.Feishu, "ix_card", proto.IxPlan); !errors.Is(err, errNoOwnerAction) {
			t.Fatalf("an action never issued: %v", err)
		}
	})
}

// The assistant's interactions.owner_action carries the channel and the card's kind; a call without them (an
// assistant of before #5011) is refused.
func TestOwnerActionCallNeedsChannelAndKind(t *testing.T) {
	h := newTestManager(t)
	call := func(params map[string]any) error {
		p, _ := json.Marshal(params)
		_, _, err := h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppInteractions,
			Method: assistantwire.InteractionsOwnerAction, Params: p})
		return err
	}
	a := h.issueMessageAction(channel.Feishu, proto.IxQuestion)
	if err := call(map[string]any{"interaction_id": "ix_q", "action": a}); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("without channel and kind: %v", err)
	}
	if err := call(map[string]any{"interaction_id": "ix_q", "action": a, "channel": channel.Feishu, "kind": proto.IxQuestion}); err != nil {
		t.Fatalf("with them: %v", err)
	}
}
