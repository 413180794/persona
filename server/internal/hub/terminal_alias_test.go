package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// A page that opened a terminal by the id an absorbed hub.json entry used to have watches the same relay as one that
// asked by the computer's own id: it gets the node's frames (which come in under that id), hears that the terminal
// ended, and its relay goes when the node drops. Each page gets them under the id it asked with, which is what the web
// terminal matches a push by (web/src/views/session/terminal/feed.ts).
func TestTerminalSubscribedByAbsorbedIDGetsFramesAndEnds(t *testing.T) {
	h := newTestHub(t)
	var mu sync.Mutex
	watches := 0
	boardNode(t, h, "canonical", func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "terminal.watch" {
			mu.Lock()
			watches++
			mu.Unlock()
		}
		return map[string]any{"control": false}, nil
	})
	h.devices.absorbed = map[string]string{"alias": "canonical"}
	a := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}}
	b := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}}
	sub := func(c *client, asked string) {
		h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_subscribe","machine_id":"`+asked+`","session_id":"s1"}`))
	}
	sub(a, "alias")
	sub(b, "canonical")
	h.terms.mu.Lock()
	n := len(h.terms.relays)
	h.terms.mu.Unlock()
	if n != 1 {
		t.Fatalf("relays = %d, want one for both pages", n)
	}

	frame, _ := json.Marshal(map[string]any{"session_id": "s1", "epoch": "e1", "seq": 1, "full": true, "cols": 80, "rows": 24, "data": "eA=="})
	h.onTerminalFrame("canonical", frame)
	next := func(c *client) map[string]any {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case raw := <-c.out:
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["type"] == "terminal" {
					return m
				}
			case <-deadline:
				t.Fatal("no terminal message")
				return nil
			}
		}
	}
	for c, asked := range map[*client]string{a: "alias", b: "canonical"} {
		if m := next(c); m["seq"] != float64(1) || m["machine_id"] != asked {
			t.Fatalf("frame = %v, want seq 1 under %q", m, asked)
		}
	}

	h.terminalsEnded("canonical", "gone")
	for c, asked := range map[*client]string{a: "alias", b: "canonical"} {
		if m := next(c); m["error"] != "gone" || m["machine_id"] != asked {
			t.Fatalf("end = %v, want error gone under %q", m, asked)
		}
	}
	h.terms.mu.Lock()
	n = len(h.terms.relays)
	h.terms.mu.Unlock()
	if n != 0 {
		t.Fatalf("relays = %d after the node dropped, want none", n)
	}
}

// Input and unsubscribe by the absorbed id reach the relay the subscribe made.
func TestTerminalUnsubscribeByAbsorbedIDDropsTheRelay(t *testing.T) {
	h := newTestHub(t)
	boardNode(t, h, "canonical", func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]any{"control": false}, nil
	})
	h.devices.absorbed = map[string]string{"alias": "canonical"}
	c := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}}
	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_subscribe","machine_id":"alias","session_id":"s1"}`))
	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_unsubscribe","machine_id":"alias","session_id":"s1"}`))
	h.terms.mu.Lock()
	n := len(h.terms.relays)
	h.terms.mu.Unlock()
	if n != 0 {
		t.Fatalf("relays = %d after unsubscribe, want none", n)
	}
}

// One app with the terminal open by both ids (two pages on one connection) gets each frame once per id; the page that
// drives it by the alias hears its control state under the alias; and closing one page leaves the other watching.
func TestTerminalOneAppByBothIDsKeepsEachPage(t *testing.T) {
	h := newTestHub(t)
	boardNode(t, h, "canonical", func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		return map[string]any{"control": true}, nil
	})
	h.devices.absorbed = map[string]string{"alias": "canonical"}
	c := termClient(h, roleFull)
	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_subscribe","machine_id":"canonical","session_id":"s1"}`))
	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_subscribe","machine_id":"alias","session_id":"s1","control":{"cols":80,"rows":24}}`))
	next := func(typ string) map[string]any {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case raw := <-c.out:
				var m map[string]any
				_ = json.Unmarshal(raw, &m)
				if m["type"] == typ {
					return m
				}
			case <-deadline:
				t.Fatalf("no %s message", typ)
				return nil
			}
		}
	}
	if m := next("terminal_control"); m["state"] != "active" || m["machine_id"] != "alias" {
		t.Fatalf("control = %v, want active under the alias", m)
	}

	frame, _ := json.Marshal(map[string]any{"session_id": "s1", "epoch": "e1", "seq": 1, "full": true, "cols": 80, "rows": 24, "data": "eA=="})
	h.onTerminalFrame("canonical", frame)
	got := map[any]bool{next("terminal")["machine_id"]: true, next("terminal")["machine_id"]: true}
	if !got["alias"] || !got["canonical"] {
		t.Fatalf("frames went under %v, want one under each id", got)
	}

	h.onClientMessage(context.Background(), c, []byte(`{"type":"terminal_unsubscribe","machine_id":"alias","session_id":"s1"}`))
	h.terms.mu.Lock()
	r := h.terms.relays[[2]string{"canonical", "s1"}]
	still := r != nil && r.subs[c]["canonical"] && !r.subs[c]["alias"] && r.controller == nil
	h.terms.mu.Unlock()
	if !still {
		t.Fatal("after the alias page closed, want the relay kept for the other page, watching only")
	}
	h.onTerminalFrame("canonical", frame)
	if m := next("terminal"); m["machine_id"] != "canonical" {
		t.Fatalf("frame = %v, want it under the computer's own id", m)
	}
	select {
	case raw := <-c.out:
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["type"] == "terminal" {
			t.Fatalf("a frame for the closed page too: %v", m)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

// awaitControls waits for n terminal_control messages and returns them as "state@machine_id".
func awaitControls(t *testing.T, c *client, n int) []string {
	t.Helper()
	var got []string
	deadline := time.After(3 * time.Second)
	for len(got) < n {
		select {
		case b := <-c.out:
			var m struct {
				Type      string `json:"type"`
				State     string `json:"state"`
				MachineID string `json:"machine_id"`
			}
			_ = json.Unmarshal(b, &m)
			if m.Type == "terminal_control" {
				got = append(got, m.State+"@"+m.MachineID)
			}
		case <-deadline:
			t.Fatalf("control messages %v, want %d", got, n)
		}
	}
	return got
}

// Two pages of one app that both drive the terminal, one by an absorbed id and one by the computer's own: the newest
// drives, as between devices. The page before hears taken under its id, the new one active under its own, and only
// what the new one types reaches the computer.
func TestTerminalOneAppDrivingByBothIDsHandsControlOver(t *testing.T) {
	tn := newTermNode(t)
	h, ctx := tn.hub, context.Background()
	h.devices.absorbed = map[string]string{"alias": "m1"}
	c := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, c, "alias", "s1", "", 0, &termCtl{80, 24}, false)
	if got := awaitControls(t, c, 1); got[0] != "active@alias" {
		t.Fatalf("control = %v, want active under the alias", got)
	}
	h.terminalSubscribe(ctx, c, "m1", "s1", "", 0, &termCtl{80, 24}, false)
	got := map[string]bool{}
	for _, s := range awaitControls(t, c, 2) {
		got[s] = true
	}
	if !got["taken@alias"] || !got["active@m1"] {
		t.Fatalf("control = %v, want taken under the alias and active under the computer's own id", got)
	}
	h.terminalInput(c, "alias", "s1", "old\r", nil, 0, nil)
	h.terminalInput(c, "m1", "s1", "new\r", nil, 0, nil)
	log := tn.await(t, 2)
	for _, e := range log {
		if strings.Contains(e, "old") {
			t.Fatalf("node log %q: the page that lost control still typed", log)
		}
	}
	if !strings.Contains(log[len(log)-1], "new") {
		t.Fatalf("node log %q, want the driving page's keys", log)
	}
}

// A device that takes over a terminal at the size the node already drives it at is still told it drives it.
func TestTerminalTakeoverAtTheSameSizeIsActive(t *testing.T) {
	tn := newTermNode(t)
	h, ctx := tn.hub, context.Background()
	phone := &client{out: make(chan []byte, 64)}
	pad := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, phone, "m1", "s1", "", 0, &termCtl{80, 24}, false)
	awaitStates(t, phone, "active")
	h.terminalSubscribe(ctx, pad, "m1", "s1", "", 0, &termCtl{80, 24}, false)
	awaitStates(t, pad, "active")
	awaitStates(t, phone, "taken")
}

// An app that goes away after the id it asked by stopped reaching the computer (the absorbed entry was forgotten)
// still leaves the relay: nothing of it stays behind.
func TestTerminalDropAfterTheAliasWentLeavesNoRelay(t *testing.T) {
	tn := newTermNode(t)
	h, ctx := tn.hub, context.Background()
	h.devices.absorbed = map[string]string{"alias": "m1"}
	c := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, c, "alias", "s1", "", 0, nil, false)
	tn.await(t, 1)
	h.devices.mu.Lock()
	h.devices.absorbed = nil
	h.devices.mu.Unlock()
	h.terminalDrop(c)
	h.terms.mu.Lock()
	n := len(h.terms.relays)
	h.terms.mu.Unlock()
	if n != 0 {
		t.Fatalf("relays = %d after the app went, want none", n)
	}
}
