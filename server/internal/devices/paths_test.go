package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
)

// Regrouping while the last regroup's ids are still being saved: the saved map is a copy (go test -race), and the
// newest ids are the ones kept.
func TestFleetRegroupSavesMachineIDsWhileRegrouping(t *testing.T) {
	st := newMemStore()
	// No context: the members' computers are made but not run.
	d := New(Options{Hub: &fakeHost{}, Store: st})
	members := func(round int) {
		var ms []devicewire.RecMachine
		for i := range 4 {
			ms = append(ms, devicewire.RecMachine{ID: fmt.Sprintf("c%d-%d", round, i), Kind: "ssh", Cfg: json.RawMessage(`{}`)})
		}
		d.mu.Lock()
		d.cfg = devicewire.Config{HubID: "hub-test", HubName: "test", Mesh: &devicewire.Mesh{
			Members: []devicewire.MemberRecord{{HubID: "member", Machines: ms}}}}
		d.mu.Unlock()
	}
	for round := range 200 {
		members(round)
		d.fleetRegroup()
	}
	d.fleet.mu.Lock()
	want := maps.Clone(d.fleet.ids)
	d.fleet.mu.Unlock()
	if len(want) != 4 {
		t.Fatalf("ids: %v", want)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var saved map[string]string
		_, _ = st.Setting(context.Background(), "mesh.machine_ids", &saved)
		if maps.Equal(saved, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved %v, want the last regroup's %v", saved, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
