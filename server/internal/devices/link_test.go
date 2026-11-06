package devices

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// endpoints carries the hub's heartbeat (hub.json link) only when it sets one: a hub without it sends what it always
// did. A change of heartbeat is something to tell the links again.
func TestLinkEndpointsCarryTheHubsHeartbeat(t *testing.T) {
	d := newTestDevices(t)
	msg := d.linkEndpoints("")
	if b, _ := json.Marshal(msg); msg.PingMS != 0 || msg.PingTimeoutMS != 0 || strings.Contains(string(b), "ping") {
		t.Fatalf("without a heartbeat set: %s", b)
	}
	before := linkSays(d.config())
	setPing := func(pingMS, timeoutMS int) {
		d.mu.Lock()
		d.cfg.LinkPingMS, d.cfg.LinkPingTimeoutMS = pingMS, timeoutMS
		d.mu.Unlock()
	}
	setPing(30000, 90000)
	msg = d.linkEndpoints("")
	if b, _ := json.Marshal(msg); msg.Type != "endpoints" || msg.HubName != "test" ||
		!strings.Contains(string(b), `"ping_ms":30000,"ping_timeout_ms":90000`) {
		t.Fatalf("with the hub's heartbeat: %s", b)
	}
	if linkSays(d.config()) == before {
		t.Fatal("a new heartbeat is not told to the links")
	}
	// Out of range: brought into it, as Persona Link would.
	setPing(1, math.MaxInt)
	if msg := d.linkEndpoints(""); msg.PingMS != 50 || msg.PingTimeoutMS != 600000 {
		t.Fatalf("out of range: %+v", msg)
	}
	// Half of it, or nonsense: none.
	setPing(30000, 0)
	if msg := d.linkEndpoints(""); msg.PingMS != 0 || msg.PingTimeoutMS != 0 {
		t.Fatalf("half a heartbeat: %+v", msg)
	}
}
