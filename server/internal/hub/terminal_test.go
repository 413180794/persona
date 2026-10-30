package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/rpc"
)

func frames(seqs ...int) []termFrame {
	var out []termFrame
	for i, s := range seqs {
		out = append(out, termFrame{Seq: s, Full: i == 0})
	}
	return out
}

func TestTerminalReplay(t *testing.T) {
	r := &termRelay{epoch: "e1", frames: frames(4, 5, 6, 7)}
	seqs := func(fs []termFrame) []int {
		var out []int
		for _, f := range fs {
			out = append(out, f.Seq)
		}
		return out
	}
	for _, tc := range []struct {
		epoch string
		after int
		want  []int
	}{
		{"e1", 5, []int{6, 7}},       // continues after its last frame
		{"e1", 7, nil},               // has everything
		{"e1", 2, []int{4, 5, 6, 7}}, // fell behind the kept frames: from the whole one
		{"e0", 6, []int{4, 5, 6, 7}}, // another stream
		{"", 0, []int{4, 5, 6, 7}},   // first subscribe
	} {
		if got := seqs(r.replay(tc.epoch, tc.after)); !equalInts(got, tc.want) {
			t.Errorf("replay(%q, %d) = %v, want %v", tc.epoch, tc.after, got, tc.want)
		}
	}
	if (&termRelay{}).replay("", 0) != nil {
		t.Error("an empty relay replays nothing")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTerminalFramesFanOutFromAWholeFrame(t *testing.T) {
	h := &Hub{}
	a := &client{out: make(chan []byte, 16)}
	h.terms.relays = map[[2]string]*termRelay{{"m1", "s1"}: {subs: map[*client]bool{a: true}}}
	frame := func(epoch string, seq int, full bool) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"session_id": "s1", "epoch": epoch, "seq": seq, "full": full, "cols": 80, "rows": 24, "data": "eA=="})
		return b
	}
	h.onTerminalFrame("m1", frame("e1", 2, false)) // a diff before any whole frame: nothing to apply it to
	h.onTerminalFrame("m1", frame("e1", 1, true))
	h.onTerminalFrame("m1", frame("e1", 2, false))
	h.onTerminalFrame("m1", frame("e2", 1, true)) // the node started over
	h.onTerminalFrame("m1", frame("e2", 2, false))
	h.onTerminalFrame("m9", frame("e2", 3, false)) // nobody watches that machine
	var got []string
	for len(a.out) > 0 {
		var m map[string]any
		_ = json.Unmarshal(<-a.out, &m)
		got = append(got, m["epoch"].(string)+"/"+strconv.Itoa(int(m["seq"].(float64))))
	}
	want := []string{"e1/1", "e1/2", "e2/1", "e2/2"}
	if len(got) != len(want) {
		t.Fatalf("sent %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sent %v, want %v", got, want)
		}
	}
	r := h.terms.relays[[2]string{"m1", "s1"}]
	if r.epoch != "e2" || len(r.frames) != 2 || !r.frames[0].Full {
		t.Fatalf("kept %s %+v", r.epoch, r.frames)
	}

	h.terminalEnded("m1", "s1", "unknown method terminal.watch", nil)
	var end map[string]any
	_ = json.Unmarshal(<-a.out, &end)
	if end["unsupported"] != true || h.terms.relays[[2]string{"m1", "s1"}] != nil {
		t.Fatalf("end = %v", end)
	}
}

// termNode is a node as the hub sees it for terminals: terminal.watch (attach_busy while busy and not a takeover;
// control true unless old), terminal.unwatch, and the events the hub sends, all recorded in order.
type termNode struct {
	mu    sync.Mutex
	log   []string
	busy  bool
	old   bool
	hub   *Hub
	where *Machine
}

func newTermNode(t *testing.T) *termNode {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	tn := &termNode{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p struct {
			Control *struct {
				Cols, Rows int
				Takeover   bool
			} `json:"control"`
		}
		_ = json.Unmarshal(params, &p)
		tn.mu.Lock()
		defer tn.mu.Unlock()
		switch {
		case method == "terminal.watch" && p.Control != nil:
			tn.log = append(tn.log, fmt.Sprintf("control %dx%d takeover=%v", p.Control.Cols, p.Control.Rows, p.Control.Takeover))
			if tn.busy && !p.Control.Takeover {
				return nil, &rpc.Error{Code: "attach_busy", Message: "attach_busy: already has an attached client"}
			}
			if tn.old {
				return map[string]any{"cols": 172, "rows": 45}, nil
			}
			return map[string]any{"cols": p.Control.Cols, "rows": p.Control.Rows, "control": true}, nil
		case method == "terminal.watch":
			tn.log = append(tn.log, "observe")
			return map[string]any{"cols": 172, "rows": 45, "control": false}, nil
		}
		tn.log = append(tn.log, method)
		return map[string]any{}, nil
	}
	nodeSide.OnEvent = func(event string, params json.RawMessage) {
		tn.mu.Lock()
		defer tn.mu.Unlock()
		tn.log = append(tn.log, event+" "+string(params))
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	h := &Hub{byID: map[string]*Machine{}, logger: log.New(io.Discard, "", 0)}
	m := newMachine(h, MachineConfig{ID: "m1", Name: "m1"})
	m.peer = hubSide
	h.byID["m1"] = m
	tn.hub, tn.where = h, m
	return tn
}

// await waits until the node's log has n entries and returns it.
func (tn *termNode) await(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		tn.mu.Lock()
		got := append([]string(nil), tn.log...)
		tn.mu.Unlock()
		if len(got) >= n {
			time.Sleep(30 * time.Millisecond) // nothing more should come
			tn.mu.Lock()
			got = append([]string(nil), tn.log...)
			tn.mu.Unlock()
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("node log %q, want %d entries", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// controlStates are the terminal_control states a client was told, in order.
func controlStates(c *client) []string {
	var out []string
	for {
		select {
		case b := <-c.out:
			var m struct {
				Type  string `json:"type"`
				State string `json:"state"`
			}
			_ = json.Unmarshal(b, &m)
			if m.Type == "terminal_control" {
				out = append(out, m.State)
			}
		default:
			return out
		}
	}
}

func awaitStates(t *testing.T, c *client, want ...string) {
	t.Helper()
	var got []string
	deadline := time.Now().Add(3 * time.Second)
	for len(got) < len(want) && time.Now().Before(deadline) {
		got = append(got, controlStates(c)...)
		time.Sleep(5 * time.Millisecond)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("control states %v, want %v", got, want)
	}
}

func TestTerminalControlDrivesAndGivesBack(t *testing.T) {
	tn := newTermNode(t)
	h, ctx := tn.hub, context.Background()
	a := &client{out: make(chan []byte, 64)}
	b := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, a, "m1", "s1", "", 0, &termCtl{44, 30}, false)
	awaitStates(t, a, "active")
	h.terminalInput(a, "m1", "s1", "ls\r", nil, 0, nil)
	h.terminalInput(b, "m1", "s1", "whoami\r", nil, 0, nil)      // b does not drive it
	h.terminalInput(a, "m1", "s1", "", &termCtl{44, 18}, 0, nil) // the keyboard came up
	h.terminalInput(a, "m1", "s1", "", nil, 3, []int{5, 9})      // a swipe down the screen: the wheel, back in history, there
	h.terminalInput(a, "m1", "s1", "", nil, -2, nil)             // an older app: no cell (the node takes the middle)
	h.terminalInput(b, "m1", "s1", "", nil, -2, []int{1, 1})
	h.terminalClick(a, "m1", "s1", 5, 7) // a tap on the screen: a click there
	h.terminalClick(b, "m1", "s1", 1, 1)
	h.terminalSubscribe(ctx, b, "m1", "s1", "", 0, nil, false)
	got := tn.await(t, 6)
	want := []string{"control 44x30 takeover=false", `terminal.input {"session_id":"s1","text":"ls\r"}`,
		`terminal.resize {"cols":44,"rows":18,"session_id":"s1"}`, `terminal.scroll {"column":5,"lines":3,"row":9,"session_id":"s1","up":true}`,
		`terminal.scroll {"lines":2,"session_id":"s1","up":false}`, `terminal.click {"column":5,"row":7,"session_id":"s1"}`}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("node got %q, want %q", got, want)
	}
	h.terminalUnsubscribe(a, "m1", "s1") // b still watches: back to the pane's own size
	if got := tn.await(t, 7); got[6] != "observe" {
		t.Fatalf("after the driver left: %q", got)
	}
	h.terminalUnsubscribe(b, "m1", "s1")
	if got := tn.await(t, 8); got[7] != "terminal.unwatch" {
		t.Fatalf("after the last left: %q", got)
	}
	h.terminalInput(a, "m1", "s1", "x", nil, 0, nil) // gone: nothing reaches the node
	if got := tn.await(t, 8); len(got) != 8 {
		t.Fatalf("typed into a terminal nobody drives: %q", got)
	}
}

func TestTerminalControlAsksBeforeTakingOver(t *testing.T) {
	tn := newTermNode(t)
	tn.busy = true
	h, ctx := tn.hub, context.Background()
	a := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, a, "m1", "s1", "", 0, &termCtl{44, 30}, false)
	awaitStates(t, a, "busy")
	if got := tn.await(t, 2); got[1] != "observe" { // watched meanwhile
		t.Fatalf("node got %q", got)
	}
	h.terminalSubscribe(ctx, a, "m1", "s1", "", 0, &termCtl{44, 30}, true) // its user said take it
	awaitStates(t, a, "active")
	if got := tn.await(t, 3); got[2] != "control 44x30 takeover=true" {
		t.Fatalf("node got %q", got)
	}
	// Someone on the computer takes it back: the phone hears so and watches.
	h.terminalControlEnded("m1", "s1", "taken_over", true)
	awaitStates(t, a, "taken")
	if got := tn.await(t, 4); got[3] != "observe" {
		t.Fatalf("node got %q", got)
	}
}

func TestTerminalControlOnAnOldNodeOnlyWatches(t *testing.T) {
	tn := newTermNode(t)
	tn.old = true
	h := tn.hub
	a := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(context.Background(), a, "m1", "s1", "", 0, &termCtl{44, 30}, false)
	awaitStates(t, a, "unsupported")
	h.terminalInput(a, "m1", "s1", "x", nil, 0, nil)
	if got := tn.await(t, 1); len(got) != 1 {
		t.Fatalf("node got %q", got)
	}
}

func TestTerminalControlNewestDeviceDrives(t *testing.T) {
	tn := newTermNode(t)
	h, ctx := tn.hub, context.Background()
	phone := &client{out: make(chan []byte, 64)}
	pad := &client{out: make(chan []byte, 64)}
	h.terminalSubscribe(ctx, phone, "m1", "s1", "", 0, &termCtl{44, 30}, false)
	awaitStates(t, phone, "active")
	h.terminalSubscribe(ctx, pad, "m1", "s1", "", 0, &termCtl{100, 40}, false)
	awaitStates(t, pad, "active")
	awaitStates(t, phone, "taken")
	if got := tn.await(t, 2); got[1] != "control 100x40 takeover=false" {
		t.Fatalf("node got %q", got)
	}
}
