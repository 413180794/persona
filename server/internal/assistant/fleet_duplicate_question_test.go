package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// #4429: the hub refuses a card with two same-text questions — none is registered or shown.
func TestNodeInteractionRejectsDuplicateQuestions(t *testing.T) {
	ft := newFleetTestNoStore(t)
	ix := proto.Interaction{ID: "ix_dup", MachineID: "mac", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "确认？"}, {Question: " 　确认？　 ", MultiSelect: true}}}
	_, err := ft.s.nodeInteraction(context.Background(), ix)
	if err == nil || !strings.Contains(err.Error(), "题干完全一样") {
		t.Fatalf("duplicate questions must be refused: %v", err)
	}
	if len(ft.s.pendingInteractions()) != 0 || len(ft.toldOf(assistantwire.EventFleetInteraction)) != 0 {
		t.Fatalf("no duplicate card may be pending or shown: %+v", ft.s.pendingInteractions())
	}
}
