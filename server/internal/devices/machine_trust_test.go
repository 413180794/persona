package devices

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
)

func TestMachineTrustIncludesEveryPath(t *testing.T) {
	for _, trust := range []string{"", "personal", "work"} {
		t.Run(trust, func(t *testing.T) {
			d := New(Options{Hub: &fakeHost{}})
			personal := newMachine(d, MachineConfig{ID: "personal", Kind: "ssh"})
			alias := newMachine(d, MachineConfig{ID: "alias", Kind: "ssh", Trust: trust})
			d.addEntry(personal)
			d.addEntry(alias)
			d.fleet.init()
			d.fleet.uids["cfg:personal"], d.fleet.uids["cfg:alias"] = "same-computer", "same-computer"
			d.fleetRegroup()
			for _, id := range []string{"personal", "alias"} {
				if got := d.IsWork(id); got != (trust == "work") {
					t.Fatalf("%s work = %v", id, got)
				}
			}
			if list := d.List(); len(list) != 1 || list[0].Trust != trust {
				t.Fatalf("summary: %+v", list)
			}
			if views := d.Views(); len(views) != 1 || views[0].Trust != trust {
				t.Fatalf("views: %+v", views)
			}
		})
	}
}

func TestDerivedMachineTrustFollowsCurrentMemberConfig(t *testing.T) {
	d := New(Options{Hub: &fakeHost{}})
	var retained *Machine
	for _, trust := range []string{"", "work", "personal"} {
		cfg, _ := json.Marshal(MachineConfig{ID: "remote", Trust: trust})
		d.cfg.Mesh = &devicewire.Mesh{Members: []devicewire.MemberRecord{{HubID: "member", Machines: []devicewire.RecMachine{{ID: "remote", Cfg: cfg}}}}}
		d.fleetRegroup()
		list := d.List()
		if len(list) != 1 || list[0].Trust != trust || d.IsWork(list[0].ID) != (trust == "work") {
			t.Fatalf("%s summary: %+v", trust, list)
		}
		m := d.machineByID(list[0].ID)
		if retained != nil && retained != m {
			t.Fatal("the test must exercise a retained derived machine")
		}
		retained = m
	}
}
