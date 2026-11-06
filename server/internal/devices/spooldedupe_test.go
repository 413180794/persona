package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
)

func TestSpoolRepeatsAreDropped(t *testing.T) {
	var s spoolSeen
	if s.repeat(json.RawMessage(`{"event":"Stop"}`)) || s.repeat(json.RawMessage(`{"event":"Stop"}`)) {
		t.Fatal("an event without spool_id is never a repeat")
	}
	if s.repeat(json.RawMessage(`{"spool_id":"a"}`)) {
		t.Fatal("the first time is not a repeat")
	}
	if !s.repeat(json.RawMessage(`{"spool_id":"a"}`)) {
		t.Fatal("the second time is")
	}
	for i := range spoolSeenMost {
		s.repeat(json.RawMessage(fmt.Sprintf(`{"spool_id":"x%d"}`, i)))
	}
	if s.repeat(json.RawMessage(`{"spool_id":"a"}`)) || len(s.order) > spoolSeenMost {
		t.Fatalf("bounded: %d remembered, the oldest forgotten", len(s.order))
	}
}

func TestMachineDropsAReplayedNotifyTwice(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box.invalid"})
	params := json.RawMessage(`{"title":"t","spool_id":"s1"}`)
	forwarded := 0
	fail := true
	d.opts.Hub.(*fakeHost).request = func(context.Context, devicewire.NodeCall) (json.RawMessage, error) {
		forwarded++
		if fail {
			return nil, errors.New("hub busy")
		}
		return json.RawMessage(`{"sent":true}`), nil
	}
	if _, err := m.onRequest(t.Context(), "notify", params); err == nil {
		t.Fatal("the hub's failure is the node's")
	}
	fail = false
	if _, err := m.onRequest(t.Context(), "notify", params); err != nil || forwarded != 2 {
		t.Fatalf("a replay after a failed one goes on: %v, forwarded %d", err, forwarded)
	}
	out, err := m.onRequest(t.Context(), "notify", params)
	if r, _ := out.(map[string]any); err != nil || r["duplicate"] != true || forwarded != 2 {
		t.Fatalf("the repeat: %v %v, forwarded %d", out, err, forwarded)
	}
}

func TestSpoolNotifyInFlight(t *testing.T) {
	var s spoolSeen
	if dup, busy := s.begin("a"); dup || busy {
		t.Fatal("first")
	}
	if dup, busy := s.begin("a"); dup || !busy {
		t.Fatal("a second try while the first is with the hub is busy, not sent")
	}
	s.end("a", false)
	if dup, busy := s.begin("a"); dup || busy {
		t.Fatal("after a failed try it goes again")
	}
	s.end("a", true)
	if dup, _ := s.begin("a"); !dup {
		t.Fatal("after it went, a repeat")
	}
}
