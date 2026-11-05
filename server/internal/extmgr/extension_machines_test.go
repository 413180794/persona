package extmgr

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// Machine names typed into a plugin's machine list become the machines' ids; what names none, or several, stays.
func TestExtensionMachineNamesBecomeIDs(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	for _, m := range []MachineConfig{{ID: "studio", Name: "Mac Studio"}, {ID: "mini", Name: "Mac mini", SSHHost: "mini.local"},
		{ID: "server", Name: "服务器"}, {ID: "laptop-a", Name: "Laptop"}, {ID: "laptop-b", Name: "laptop"}} {
		mm := newMachine(h, m)
		mm.info = proto.NodeInfo{OS: "darwin"}
		h.machines, h.byID[m.ID] = append(h.machines, mm), mm
	}
	if err := h.extensions.reg.SetConfig(ctx, cuaExtensionID, json.RawMessage(`{"machines": ["macstudio", "mac-mini", "studio", "nowhere", "laptop"]}`), "test"); err != nil {
		t.Fatal(err)
	}
	h.fixMachineSettings(ctx, h.extensions.reg)
	var got struct{ Machines []string }
	_ = json.Unmarshal(h.extensions.reg.Get(cuaExtensionID).Record.Config, &got)
	want := []string{"studio", "mini", "nowhere", "laptop"}
	if len(got.Machines) != len(want) {
		t.Fatalf("%v", got.Machines)
	}
	for i := range want {
		if got.Machines[i] != want[i] {
			t.Fatalf("%v, want %v", got.Machines, want)
		}
	}
	if h.machineIDFor("mini.local") != "mini" || h.machineIDFor("MAC_STUDIO") != "studio" || h.machineIDFor("laptop") != "" {
		t.Fatal("by host, by name with case and spaces ignored; two machines called laptop: neither")
	}
}
