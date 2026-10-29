package hub

import (
	"encoding/json"
	"strconv"
	"testing"
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
