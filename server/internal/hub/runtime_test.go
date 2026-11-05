package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A runtime's card is pushed under its own name.
func TestRuntimePushTitle(t *testing.T) {
	p := interactionPayload(proto.Interaction{Kind: proto.IxPermission, Agent: "dsh", AgentLabel: "DSH", Summary: "echo hi"}, "server")
	alert := p["aps"].(map[string]any)["alert"].(map[string]any)
	if alert["title"] != "DSH 需要你授权" {
		t.Errorf("%v", alert)
	}
}
