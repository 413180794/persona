//go:build herdrlive

package herdr

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Against the herdr running on this machine: go test -tags herdrlive ./internal/herdr/
func TestLiveRPCMatchesCLI(t *testing.T) {
	ctx := context.Background()
	rpc := New()
	cli := &Client{Bin: "herdr"}
	t0 := time.Now()
	a, err := rpc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rpcTook := time.Since(t0)
	t0 = time.Now()
	b, err := cli.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot: socket %s, CLI %s; %d agents, %d panes", rpcTook, time.Since(t0), len(a.Agents), len(a.Panes))
	if len(a.Agents) != len(b.Agents) || len(a.Panes) != len(b.Panes) || len(a.Tabs) != len(b.Tabs) {
		t.Fatalf("socket and CLI snapshots differ: %d/%d agents", len(a.Agents), len(b.Agents))
	}
	if len(a.Agents) == 0 {
		t.Skip("no agents to read")
	}
	pane := a.Agents[0].PaneID
	x, err := rpc.PaneRead(ctx, pane, "visible", 60)
	if err != nil {
		t.Fatal(err)
	}
	y, err := cli.PaneRead(ctx, pane, "visible", 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(x) == 0 || len(y) == 0 {
		t.Fatalf("empty reads %d %d", len(x), len(y))
	}
	if _, err := rpc.PaneRead(ctx, pane, "recent-unwrapped", 30); err != nil {
		t.Fatal(err)
	}
	pids, err := rpc.PaneProcesses(ctx, pane)
	t.Logf("pane %s pids %v %v; version %q", pane, pids, err, rpc.Version(ctx))
	if _, err := rpc.AgentGet(ctx, "w999:p999"); !IsNotFound(err) {
		t.Fatalf("missing agent: %v", err)
	}
}

func TestLiveSubscribe(t *testing.T) {
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 16)
	go func() {
		_ = c.Subscribe(ctx, []Subscription{{"type": "tab.created"}, {"type": "tab.closed"}, {"type": "tab.renamed"}}, func(e Event) { got <- e.Event })
	}()
	if e := <-got; e != SubscriptionStarted {
		t.Fatalf("first event %q", e)
	}
	ws, err := c.Workspaces(ctx)
	if err != nil || len(ws) == 0 {
		t.Fatal(err)
	}
	created, err := c.TabCreate(ctx, ws[0].ID, "/tmp", "jarvis-live-test")
	if err != nil {
		t.Fatal(err)
	}
	defer exec.Command("herdr", "tab", "close", created.Tab.ID).Run()
	select {
	case e := <-got:
		if e != "tab_created" { // subscribed as tab.created, reported as tab_created
			t.Fatalf("event %q", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no tab.created event")
	}
	if err := c.TabClose(ctx, created.Tab.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		t.Logf("then %q", e)
	case <-time.After(3 * time.Second):
		t.Fatal("no tab.closed event")
	}
}

// An observer of a live pane gets a whole frame at the pane's size first, and the pane keeps its size.
func TestLiveObserve(t *testing.T) {
	ctx := context.Background()
	c := New()
	snap, err := c.Snapshot(ctx)
	if err != nil || len(snap.Agents) == 0 {
		t.Skip("no agent pane to observe")
	}
	pane := snap.Agents[0].PaneID
	cols, rows, err := c.PaneSize(ctx, pane)
	if err != nil {
		t.Fatal(err)
	}
	octx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	var got []Frame
	_ = c.Observe(octx, pane, cols, rows, func(f Frame) { got = append(got, f) })
	if len(got) == 0 || !got[0].Full || got[0].Width != cols || got[0].Height != rows {
		t.Fatalf("frames from %s (%d×%d): %+v", pane, cols, rows, got)
	}
	if c2, r2, _ := c.PaneSize(ctx, pane); c2 != cols || r2 != rows {
		t.Fatalf("observing resized %s: %d×%d → %d×%d", pane, cols, rows, c2, r2)
	}
	t.Logf("%s %d×%d: %d frames in 1.5 s, first %d bytes (base64)", pane, cols, rows, len(got), len(got[0].Data))
}
