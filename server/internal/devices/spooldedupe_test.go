package devices

import (
	"encoding/json"
	"fmt"
	"testing"
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
	m := newMachine(newTestDevices(t), MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box.invalid"})
	params := json.RawMessage(`{"title":"t","spool_id":"s1"}`)
	_, _ = m.onRequest(t.Context(), "notify", params) // the first goes on to the hub (the fake's answer)
	out, err := m.onRequest(t.Context(), "notify", params)
	if r, _ := out.(map[string]any); err != nil || r["duplicate"] != true {
		t.Fatalf("the repeat: %v %v", out, err)
	}
}
