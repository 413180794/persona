package hub

import (
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/mdns"
	"testing"
	"time"
)

func TestCompatibleDiscoveryGoodbyeDoesNotRemoveOtherService(t *testing.T) {
	var output []mdns.Entry
	d := &compatibleDiscovery{publish: func(e mdns.Entry) { output = append(output, e) }}
	old, current := d.found(compat.DiscoveryService), d.found(meshService)
	e := mdns.Entry{TXT: map[string]string{"hub_id": "fixture-hub"}, TTL: time.Minute, Seen: time.Now()}
	old(e)
	current(e)
	e.TTL = 0
	old(e)
	if output[len(output)-1].TTL == 0 {
		t.Fatal("one service goodbye removed a live identity")
	}
	if len(d.entries) != 1 {
		t.Fatal("identity was duplicated or lost")
	}
	current(e)
	if output[len(output)-1].TTL != 0 || len(d.entries) != 0 {
		t.Fatal("last goodbye did not remove identity")
	}
	// Unknown goodbyes cannot allocate identities or evict a live service.
	e.TXT = map[string]string{"hub_id": "unknown"}
	old(e)
	if len(output) != 4 || len(d.entries) != 0 {
		t.Fatal("unknown goodbye altered discovery")
	}
}
