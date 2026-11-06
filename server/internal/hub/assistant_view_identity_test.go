package hub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestAssistantSingleSessionUsesTrustedClassification(t *testing.T) {
	h, n := conversationsHub(t)
	editSessions(n.m, func([]proto.Session) []proto.Session {
		return []proto.Session{
			{SessionID: "headless", Cwd: convDir},
			{SessionID: "pane", Cwd: convDir, PaneID: "window:pane", Assistant: true},
			{SessionID: "ordinary", Cwd: "/code/example"},
		}
	})
	for _, tc := range []struct {
		id   string
		want bool
	}{{"headless", true}, {"pane", false}, {"ordinary", false}} {
		raw, err := (assistantLink{h}).DeviceCall(context.Background(), assistantwire.DeviceCall{Device: n.m.cfg.ID, Method: assistantwire.DeviceSessionGet, Params: json.RawMessage(`{"session_id":"` + tc.id + `"}`)})
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Session proto.Session
			OK      bool
		}
		if err := json.Unmarshal(raw, &result); err != nil || !result.OK || result.Session.Assistant != tc.want {
			t.Fatalf("%s: %+v %v", tc.id, result, err)
		}
	}
}
