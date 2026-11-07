package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/rpc"
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

// A replayed notification the manager cannot hand to the hub because it has no connection to it (or the link was
// down before the call went out) is answered hub_away, which the node keeps without counting a try; the hub's own
// refusal goes back as it is. One whose link dropped after it went out may have been pushed: it is taken for sent, so
// a try again is a repeat, never a second push.
func TestReplayedNotifyWhileTheHubIsAway(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box.invalid"})
	up := false
	d.opts.Connected = func() bool { return up }
	fail := errors.New("not connected to the hub")
	forwarded := 0
	d.opts.Hub.(*fakeHost).request = func(context.Context, devicewire.NodeCall) (json.RawMessage, error) {
		forwarded++
		if fail != nil {
			return nil, fail
		}
		return json.RawMessage(`{"sent":true}`), nil
	}
	send := func(id string) (map[string]any, string, error) {
		t.Helper()
		out, err := m.onRequest(t.Context(), "notify", json.RawMessage(`{"title":"t","spool_id":"`+id+`"}`))
		var rerr *rpc.Error
		if errors.As(err, &rerr) {
			return nil, rerr.Code, err
		}
		r, ok := out.(map[string]any) // a repeat's own answer; the hub's comes as it sent it
		if raw, isRaw := out.(json.RawMessage); isRaw && !ok {
			_ = json.Unmarshal(raw, &r)
		}
		return r, "", err
	}
	if _, c, err := send("s1"); err == nil || c != nodeproto.ErrHubAway {
		t.Fatalf("hub away: %v, code %q", err, c)
	}
	up = true
	fail = fmt.Errorf("the link is down: %w", rpc.ErrClosed) // refused before it went out
	if _, c, _ := send("s2"); c != nodeproto.ErrHubAway {
		t.Fatalf("not sent before the link dropped: code %q", c)
	}
	fail = errors.New("notification is too large") // the hub's refusal
	if _, c, err := send("s3"); err == nil || c != "" {
		t.Fatalf("the hub's refusal: %v, code %q, want it as it is", err, c)
	}
	fail = nil
	if r, _, err := send("s1"); err != nil || r["sent"] != true || r["duplicate"] == true {
		t.Fatalf("a try after hub_away goes to the hub: %v %v", r, err)
	}

	fail = fmt.Errorf("the link dropped: %w", rpc.ErrDropped) // it went out: the hub may have pushed it
	if _, c, err := send("s4"); err == nil || c == nodeproto.ErrHubAway {
		t.Fatalf("dropped after it went out: %v, code %q, want an error that is not hub_away", err, c)
	}
	fail = nil
	before := forwarded
	if r, _, err := send("s4"); err != nil || r["duplicate"] != true || forwarded != before {
		t.Fatalf("a try after a drop: %v %v (forwarded %d more), want a repeat, not a second push", r, err, forwarded-before)
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
