package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

func TestInputIsActivity(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want bool
	}{
		{"a letter", "a", true},
		{"an arrow key", "\x1b[A", true},
		{"a click", "\x1b[<0;10;5M", true},
		{"a release", "\x1b[<0;10;5m", true},
		{"a drag", "\x1b[<32;10;5M", true},
		{"the wheel", "\x1b[<64;10;5M", true},
		{"the mouse passing over", "\x1b[<35;10;5M", false},
		{"several passes", "\x1b[<35;10;5M\x1b[<35;11;5M", false},
		{"a pass with shift held", "\x1b[<39;10;5M", false},
		{"a drag with the right button", "\x1b[<34;10;5M", true},
		{"a pass and a key", "\x1b[<35;10;5M\r", true},
		{"focus in", "\x1b[I", false},
		{"focus out", "\x1b[O", false},
		{"the scheme answer", "\x1b[?997;1n", false},
		{"the scheme mode answer", "\x1b[?2031;2$y", false},
		{"a colour answer", "\x1b]11;rgb:1010/1010/1010\x1b\\", false},
		{"a colour answer ended by bel", "\x1b]10;rgb:ffff/ffff/ffff\x07", false},
		{"the size answer", "\x1b[6;18;9t", false},
		{"device attributes", "\x1b[?62;4c", false},
		{"kitty keyboard answer", "\x1b[?0u", false},
		{"an answer and a key", "\x1b[?997;1ny", true},
		{"nothing", "", false},
	} {
		if got := inputIsActivity([]byte(c.in)); got != c.want {
			t.Errorf("%s: inputIsActivity(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// focusNode is a node whose herdr shows focus (set with show) and has an agent session in w1:p1, a shell in w1:p2
// (another tab of the same workspace) and one in w2:p3; hub pushes come back on the channel.
func focusNode(t *testing.T) (n *Node, f *fakeHerdr, show func(ws, tab, pane string), pushes <-chan hubPush) {
	t.Helper()
	n, f, _ = herdrNode(t)
	agent := claudeAgent(sidA, "idle", "w1:p1", "w1:t1", "w1", "/tmp/a", "")
	shell := func(pane, tab, ws string) map[string]any {
		return map[string]any{"pane_id": pane, "tab_id": tab, "workspace_id": ws, "cwd": "/tmp", "terminal_title": "sh"}
	}
	show = func(ws, tab, pane string) {
		snap := snapshot(nil, nil, []any{agent}, []any{agent, shell("w1:p2", "w1:t2", "w1"), shell("w2:p3", "w2:t1", "w2")})
		inner := snap["snapshot"].(map[string]any)
		inner["focused_workspace_id"], inner["focused_tab_id"], inner["focused_pane_id"] = ws, tab, pane
		f.reply("session.snapshot", snap)
	}
	show("w1", "w1:t1", "w1:p1")
	listSessions(t, n)
	all, pw := hubPushes(n)
	t.Cleanup(func() { pw.Close() })
	out := make(chan hubPush, 16)
	go func() {
		for m := range all {
			if m.Event == "herdr.focus.changed" {
				out <- m
			}
		}
	}()
	return n, f, show, out
}

func focuserOf(t *testing.T, n *Node) host.Focuser {
	t.Helper()
	fo, ok := n.hosts.Get(host.Herdr).(host.Focuser)
	if !ok {
		t.Fatal("the herdr host cannot report focus")
	}
	return fo
}

func nextFocus(t *testing.T, ch <-chan hubPush) map[string]any {
	t.Helper()
	select {
	case m := <-ch:
		var p map[string]any
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatal(err)
		}
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("no herdr.focus.changed")
		return nil
	}
}

func noFocus(t *testing.T, ch <-chan hubPush) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("unexpected herdr.focus.changed %s", m.Params)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFocusReportCarriesItsSource(t *testing.T) {
	n, _, show, pushes := focusNode(t)
	fo := focuserOf(t, n)
	ctx := context.Background()

	// The first read is the baseline, not a change; the same focus again is not one either.
	n.reportFocus(ctx, fo)
	n.reportFocus(ctx, fo)
	noFocus(t, pushes)

	// Moved with nobody typing in a full client: the computer's own window, or another tool.
	show("w1", "w1:t2", "w1:p2")
	n.reportFocus(ctx, fo)
	p := nextFocus(t, pushes)
	if p["pane_id"] != "w1:p2" || p["tab_id"] != "w1:t2" || p["workspace_id"] != "w1" || p["source"] != "other" || p["client_id"] != nil || p["session_id"] != nil {
		t.Fatalf("a change nobody made here = %v", p)
	}
	n.reportFocus(ctx, fo)
	noFocus(t, pushes)

	// The user just acted in a full client of this node: the change is that client's, and an agent's pane names its session.
	view := &fullClient{id: "hc_1", activeAt: time.Now()}
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": view}
	n.mu.Unlock()
	show("w1", "w1:t1", "w1:p1")
	n.reportFocus(ctx, fo)
	p = nextFocus(t, pushes)
	if p["source"] != "client" || p["client_id"] != "hc_1" || p["pane_id"] != "w1:p1" || p["session_id"] != sidA {
		t.Fatalf("a change after input = %v", p)
	}

	// Input older than the window is not the cause.
	view.mu.Lock()
	view.activeAt = time.Now().Add(-2 * focusWindow)
	view.mu.Unlock()
	show("w2", "w2:t1", "w2:p3")
	n.reportFocus(ctx, fo)
	if p = nextFocus(t, pushes); p["source"] != "other" {
		t.Fatalf("a change long after input = %v", p)
	}

	// With two views the one acted in last gets it.
	older := &fullClient{id: "hc_2", activeAt: time.Now().Add(-focusWindow / 2)}
	view.mu.Lock()
	view.activeAt = time.Now()
	view.mu.Unlock()
	n.mu.Lock()
	n.fulls["hc_2"] = older
	n.mu.Unlock()
	show("w1", "w1:t1", "w1:p1")
	n.reportFocus(ctx, fo)
	if p = nextFocus(t, pushes); p["client_id"] != "hc_1" {
		t.Fatalf("two views, the latest input = %v", p)
	}
}

func TestMouseMotionIsNotTheCauseOfAChange(t *testing.T) {
	n, _, show, pushes := focusNode(t)
	fo := focuserOf(t, n)
	ctx := context.Background()
	n.reportFocus(ctx, fo)
	view := &fullClient{id: "hc_1"}
	n.mu.Lock()
	n.fulls = map[string]*fullClient{"hc_1": view}
	n.mu.Unlock()
	view.markActive([]byte("\x1b[<35;4;4M\x1b[I"))
	show("w1", "w1:t2", "w1:p2")
	n.reportFocus(ctx, fo)
	if p := nextFocus(t, pushes); p["source"] != "other" {
		t.Fatalf("the mouse passing over a view was taken for the user acting: %v", p)
	}
	view.markActive([]byte("\x1b[<0;4;4M"))
	show("w1", "w1:t1", "w1:p1")
	n.reportFocus(ctx, fo)
	if p := nextFocus(t, pushes); p["source"] != "client" {
		t.Fatalf("a click = %v", p)
	}
}
