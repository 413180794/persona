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

// End to end through a plugin channel's real inbound path: the owner action handed over with a received message
// (channels_extension.go issues it for the channel's typed-answer kinds) approves the plan and permission asked in
// that same channel, answers a question there, cannot be spent through Feishu, and is single use. The unit-level
// card/channel/kind matrix is owner_action_test.go's; this guards the issuance half of the extension-channel
// text-approval chain (docs/extensions/channels.md"Questions and approvals"), which #5011's tightening must not break.
func TestExtensionChannelTextApprovalEndToEnd(t *testing.T) {
	w := newChannelWorld(t)
	// A message in the plugin channel: the action the hub hands the assistant with it.
	actionFor := func(text string) string {
		before := len(w.received())
		w.receive(t, "m_"+text, text)
		eventually(t, 5*time.Second, "the message handed to the assistant", func() bool { return len(w.received()) > before })
		return w.received()[before].Action
	}
	ownerAction := func(action, kind, ch string) error {
		p, _ := json.Marshal(map[string]any{"interaction_id": "ix_card", "action": action, "kind": kind, "channel": ch})
		_, _, err := w.h.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppInteractions,
			Method: assistantwire.InteractionsOwnerAction, Params: p})
		return err
	}
	if err := ownerAction(actionFor("批准"), proto.IxPlan, "examplechat"); err != nil {
		t.Fatalf("the channel's plan approval refused: %v", err)
	}
	if err := ownerAction(actionFor("允许"), proto.IxPermission, "examplechat"); err != nil {
		t.Fatalf("the channel's permission approval refused: %v", err)
	}
	if err := ownerAction(actionFor("北京"), proto.IxQuestion, "examplechat"); err != nil {
		t.Fatalf("the channel's question answer refused: %v", err)
	}
	// The same message action cannot answer a card through another channel, even for a kind that channel takes.
	if err := ownerAction(actionFor("1"), proto.IxPlan, channel.Feishu); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("the channel's action spent through Feishu: %v", err)
	}
	last := actionFor("2")
	if err := ownerAction(last, proto.IxPlan, "examplechat"); err != nil {
		t.Fatalf("spend: %v", err)
	}
	if err := ownerAction(last, proto.IxPlan, "examplechat"); !errors.Is(err, errNoOwnerAction) {
		t.Fatalf("spent twice: %v", err)
	}
}
