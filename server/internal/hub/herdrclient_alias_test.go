package hub

import (
	"encoding/json"
	"fmt"
	"testing"
)

// A view opened by an id another machine absorbed belongs to the machine it reached: the node's data, end and
// going offline all come in under that machine's own id.
func TestHerdrViewOpenedByAnAbsorbedIDFollowsItsMachine(t *testing.T) {
	fn := newFullNode(t, nil)
	h := fn.hub
	h.devices.absorbed = map[string]string{"alias": "m1"}
	a := termClient(h, roleFull)
	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "alias", "id": "v1", "cols": 80, "rows": 24})
	fn.await(t, 1)
	awaitTold(t, a, 1) // herdr_state
	id := nodeID(t, h)

	h.onHerdrClientData("m1", dataMsg(id, b64("x")))
	if m := awaitTold(t, a, 1)[0]; m["type"] != "herdr_data" || m["id"] != "v1" || m["data"] != b64("x") {
		t.Fatalf("told %v", m)
	}

	h.onHerdrClientEnd("m1", json.RawMessage(fmt.Sprintf(`{"client_id":%q,"error":"herdr client exited: 1"}`, id)))
	if m := awaitTold(t, a, 1)[0]; m["type"] != "herdr_end" || m["id"] != "v1" {
		t.Fatalf("told %v", m)
	}
	if views(h) != 0 {
		t.Fatal("an ended view stays registered")
	}

	say(h, a, map[string]any{"type": "herdr_open", "machine_id": "alias", "id": "v2", "cols": 80, "rows": 24})
	fn.await(t, 2)
	awaitTold(t, a, 1)
	h.fullsOffline("m1")
	if m := awaitTold(t, a, 1)[0]; m["type"] != "herdr_end" || m["id"] != "v2" || m["code"] != "offline" {
		t.Fatalf("told %v", m)
	}
	if views(h) != 0 {
		t.Fatal("a view stays on an offline machine")
	}
}
