package devices

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/managerconf"
)

func linkEntryOf(id string) json.RawMessage {
	b, _ := json.Marshal(MachineConfig{ID: id, Name: "Name of " + id, Kind: kindLink})
	return b
}

// ownListDevices is a manager's devices with file as its manager.json, run by Run until the test ends.
func ownListDevices(t *testing.T, file string) *Devices {
	t.Helper()
	d := New(Options{Hub: &fakeHost{}, Store: newMemStore(), Logf: func(string, ...any) {}, ConfigFile: file})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return d
}

// A manager whose hub does not answer connects the machines of its own list (manager.json) after ownListWait, as the
// hub's last hand-over had them; the hub's hand-over, when it comes, is the list, and is kept there. A hub that answers
// in time leaves nothing to connect before it; a list another hub handed over, or one from a mesh group, waits for the hub.
func TestTheOwnListStartsWithoutTheHub(t *testing.T) {
	was := ownListWait
	ownListWait = 100 * time.Millisecond
	t.Cleanup(func() { ownListWait = was })
	file := filepath.Join(t.TempDir(), managerconf.File)
	if _, _, _, err := managerconf.KeepMachines(file, []json.RawMessage{linkEntryOf("one"), linkEntryOf("two")},
		managerconf.MachinesFromHub{HubID: "hub-test"}); err != nil {
		t.Fatal(err)
	}

	d := ownListDevices(t, file)
	eventually(t, 3*time.Second, "the own list started", func() bool { return d.cfgMachine("one") != nil && d.cfgMachine("two") != nil })
	if _, err := d.PendingMachineIDs(context.Background()); err == nil {
		t.Fatal("the hub's word on pending computers taken as said")
	}
	one := d.cfgMachine("one")
	d.Configure(devicewire.Config{HubID: "hub-test", Machines: []json.RawMessage{linkEntryOf("one"), linkEntryOf("three")},
		Mesh: &devicewire.Mesh{}})
	if d.cfgMachine("one") != one || d.cfgMachine("two") != nil || d.cfgMachine("three") == nil {
		t.Fatal("the hub's hand-over is not the list")
	}
	list, from, ok, err := managerconf.OwnMachines(file)
	if err != nil || !ok || len(list) != 2 || !from.Mesh || from.HubID != "hub-test" {
		t.Fatalf("kept: %s %+v %v %v", list, from, ok, err)
	}

	// The hub was in a mesh group at that hand-over: the next start waits for it.
	next := ownListDevices(t, file)
	time.Sleep(4 * ownListWait)
	if next.cfgMachine("one") != nil {
		t.Fatal("a mesh group's computers connected without the hub")
	}

	// A hub that answers in time: nothing from the own list before it.
	if _, _, _, err := managerconf.KeepMachines(file, list, managerconf.MachinesFromHub{HubID: "hub-test"}); err != nil {
		t.Fatal(err)
	}
	ownListWait = time.Hour
	quick := ownListDevices(t, file)
	time.Sleep(200 * time.Millisecond)
	if quick.cfgMachine("one") != nil {
		t.Fatal("connected before the hub had the time to answer")
	}
	quick.Configure(devicewire.Config{HubID: "hub-test", Machines: []json.RawMessage{linkEntryOf("three")}})
	eventually(t, 3*time.Second, "the hub's list started", func() bool { return quick.cfgMachine("three") != nil })
	if quick.cfgMachine("one") != nil {
		t.Fatal("the own list was used with the hub there")
	}

	// The manager joined another hub since: that list is not read.
	ownListWait = 100 * time.Millisecond
	_ = managerconf.Edit(file, func(o *managerconf.Object) error { return o.Set("hub_id", "hub-other") })
	other := ownListDevices(t, file)
	time.Sleep(4 * ownListWait)
	if other.cfgMachine("three") != nil {
		t.Fatal("another hub's list connected")
	}
}
