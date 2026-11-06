package devices

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

// When a node's sessions.changed reaches Options.Event, Sessions already returns that list: the assistant takes it
// from there instead of decoding the event again (assistant NodeEvent). The same holds for BusUp's replay.
func TestSessionsEventAfterTheList(t *testing.T) {
	d := newTestDevices(t)
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
	t.Cleanup(func() { cancel(); d.workers.Wait() })
	b, _ := json.Marshal(MachineConfig{ID: "mac", Name: "mac", Kind: kindLink})
	d.Configure(devicewire.Config{Machines: []json.RawMessage{b}})
	m := d.cfgMachine("mac")
	if m == nil {
		t.Fatal("machine not started")
	}
	var seen, sent [][]proto.Session
	d.opts.Event = func(machine, event string, params json.RawMessage) bool {
		if event != "sessions.changed" {
			return true
		}
		list, _ := d.Sessions(machine)
		seen = append(seen, list)
		var p struct {
			Sessions []proto.Session `json:"sessions"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Fatal(err)
		}
		for i := range p.Sessions {
			p.Sessions[i].MachineID = machine
		}
		sent = append(sent, p.Sessions)
		return false
	}
	for _, list := range [][]proto.Session{
		{{SessionID: "s1", Title: "one", Queued: []string{"q"}}, {SessionID: "s2", SpaceName: "app"}},
		{},
		{{SessionID: "s2", SpaceName: "app"}},
	} {
		params, _ := json.Marshal(map[string]any{"sessions": list})
		m.onEvent("sessions.changed", params)
	}
	d.BusUp()
	if len(seen) != 4 {
		t.Fatalf("%d events", len(seen))
	}
	for i := range seen {
		if !reflect.DeepEqual(seen[i], sent[i]) {
			t.Fatalf("event %d: devices have %+v, the event says %+v", i, seen[i], sent[i])
		}
	}
}
