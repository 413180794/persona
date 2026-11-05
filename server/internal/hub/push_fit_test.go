package hub

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A card whose summary is longer than APNs takes (a gated Feishu write shows up to 2000 characters, about 6 KB of
// Chinese) goes out shortened on a direct push, not refused whole with 413 (#3809).
func TestDirectPushOfALongCardFitsAPNs(t *testing.T) {
	h, f := testLive(t)
	ix := proto.Interaction{ID: "ix_long", Kind: proto.IxPermission, MachineID: "m1", SessionID: "s1", ToolName: "飞书",
		Summary: strings.Repeat("以你的身份在飞书上发消息", 200)}
	h.push.notifyInteraction(ix, "box")
	got := f.calls(t, 1)[0]
	if len(got.Body) > apnsMaxAlert {
		t.Fatalf("direct push of %d bytes: APNs refuses more than %d", len(got.Body), apnsMaxAlert)
	}
	var p struct {
		APS struct {
			Alert    map[string]string `json:"alert"`
			Category string            `json:"category"`
		} `json:"aps"`
		InteractionID string `json:"interaction_id"`
	}
	if err := json.Unmarshal(got.Body, &p); err != nil {
		t.Fatal(err)
	}
	if p.APS.Category != "INTERACTION_PERMISSION" || p.InteractionID != "ix_long" || p.APS.Alert["title"] == "" ||
		!strings.HasPrefix(p.APS.Alert["body"], "以你的身份") {
		t.Fatalf("shortened push lost what it is: %+v", p)
	}
	if got.Collapse != "ix_long" {
		t.Fatalf("collapse id %q", got.Collapse)
	}
}

// A push that fits is sent as it is.
func TestDirectPushThatFitsIsUnchanged(t *testing.T) {
	h, f := testLive(t)
	ix := proto.Interaction{ID: "ix_short", Kind: proto.IxPermission, MachineID: "m1", SessionID: "s1", Summary: "rm -rf build"}
	h.push.notifyInteraction(ix, "box")
	if body := string(f.calls(t, 1)[0].Body); !strings.Contains(body, `"body":"rm -rf build"`) {
		t.Fatalf("short push changed: %s", body)
	}
}
