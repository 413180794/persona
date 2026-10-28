package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// fakeNode answers session.items after_seq from n items (seq 1..n, one item per line) the way the node does: at most
// 500 per call, more_after when items are left.
func fakeNode(t *testing.T, n int) *Machine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide := rpc.New("h", h2nW)
	nodeSide := rpc.New("n", n2hW)
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p struct {
			AfterSeq *int `json:"after_seq"`
		}
		_ = json.Unmarshal(params, &p)
		if method != "session.items" || p.AfterSeq == nil {
			return nil, fmt.Errorf("unexpected %s %s", method, params)
		}
		var items []proto.Item
		for s := *p.AfterSeq + 1; s <= n && len(items) < 500; s++ {
			items = append(items, proto.Item{ID: fmt.Sprintf("i%d", s), Seq: s, Role: "assistant", Kind: proto.KindText})
		}
		more := len(items) > 0 && items[len(items)-1].Seq < n
		return map[string]any{"items": items, "has_more": false, "more_after": more}, nil
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(&Hub{}, MachineConfig{ID: "m1", Name: "m1"})
	m.peer = hubSide
	return m
}

type batch struct {
	Items     []proto.Item `json:"items"`
	MoreAfter bool         `json:"more_after"`
	HasMore   *bool        `json:"has_more"`
}

func drain(t *testing.T, c *client) []batch {
	t.Helper()
	var out []batch
	for {
		select {
		case b := <-c.out:
			var x batch
			if err := json.Unmarshal(b, &x); err != nil {
				t.Fatal(err)
			}
			out = append(out, x)
		default:
			return out
		}
	}
}

func TestBacklogCatchUpSendsEverything(t *testing.T) {
	m := fakeNode(t, 1300)
	c := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}}
	(&Hub{}).sendBacklog(context.Background(), c, m, "s1", 10)
	batches := drain(t, c)
	next := 11
	for _, b := range batches {
		if b.MoreAfter || b.HasMore != nil {
			t.Fatalf("a complete catch-up says more_after %v / has_more %v", b.MoreAfter, b.HasMore)
		}
		for _, it := range b.Items {
			if it.Seq != next {
				t.Fatalf("got seq %d, want %d", it.Seq, next)
			}
			next++
		}
	}
	if next != 1301 {
		t.Fatalf("catch-up ended at seq %d, want everything through 1300 (used to stop after 500)", next-1)
	}
}

func TestBacklogCatchUpCapsAndSaysSo(t *testing.T) {
	m := fakeNode(t, 10000)
	c := &client{out: make(chan []byte, 64), subs: map[[2]string]bool{}}
	(&Hub{}).sendBacklog(context.Background(), c, m, "s1", 1)
	batches := drain(t, c)
	if len(batches) != backlogPages {
		t.Fatalf("%d batches, want %d", len(batches), backlogPages)
	}
	if !batches[len(batches)-1].MoreAfter {
		t.Fatal("the last batch of a capped catch-up must say more_after")
	}
	for _, b := range batches[:len(batches)-1] {
		if b.MoreAfter {
			t.Fatal("only the last batch says more_after")
		}
	}
}
