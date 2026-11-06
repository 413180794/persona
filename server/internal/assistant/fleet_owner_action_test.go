package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// Answering a card from a channel has the hub take the owner's action for that channel and the card's kind as the
// queue has it (a caller's own kind is not asked); a card no longer pending takes no action (#5011).
func TestChannelResolveTakesTheActionForItsChannelAndKind(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	refuse := false
	ft.f.answerApp(AppInteractions, assistantwire.InteractionsOwnerAction, func(json.RawMessage) (any, error) {
		if refuse {
			return nil, errors.New("no owner action")
		}
		return map[string]any{}, nil
	})
	done := make(chan proto.Resolution, 1)
	putPending(s, &pending{ix: proto.Interaction{ID: "ix_plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan},
		done: done})
	resolve := func(id string) (map[string]any, error) {
		var out map[string]any
		err := s.appCall(context.Background(), AppInteractions, InteractionsResolve, map[string]any{"interaction_id": id,
			"action": "oa_1", "channel": channel.Feishu, "kind": proto.IxQuestion,
			"resolution": map[string]any{"approve": true, "mode": "acceptEdits"}}, &out)
		return out, err
	}

	refuse = true
	if _, err := resolve("ix_plan"); err == nil {
		t.Fatal("answered though the hub refused the action")
	}
	if len(done) != 0 {
		t.Fatal("the card was answered")
	}
	calls := ft.f.appParams(AppInteractions, assistantwire.InteractionsOwnerAction)
	if len(calls) != 1 {
		t.Fatalf("owner_action calls: %v", calls)
	}
	if c := calls[0]; c["interaction_id"] != "ix_plan" || c["action"] != "oa_1" || c["channel"] != channel.Feishu || c["kind"] != proto.IxPlan {
		t.Fatalf("owner_action params: %v", c)
	}

	refuse = false
	if out, err := resolve("ix_plan"); err != nil || out["ok"] != true {
		t.Fatalf("resolve: %v %v", out, err)
	}
	if r := <-done; r.Approve == nil || !*r.Approve {
		t.Fatalf("answered with %+v", r)
	}

	if out, err := resolve("ix_gone"); err != nil || out["ok"] != false {
		t.Fatalf("a card not pending: %v %v", out, err)
	}
	if n := len(ft.f.appParams(AppInteractions, assistantwire.InteractionsOwnerAction)); n != 2 {
		t.Fatalf("an action taken for a card not pending: %d calls", n)
	}
}
