package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// fakeObserveHerdr answers pane layout with an 80×24 pane and streams a whole frame and two diffs, then ends.
func fakeObserveHerdr(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
case "$1 $2" in
"pane layout") echo '{"result":{"layout":{"panes":[{"pane_id":"w1:p1","rect":{"width":80,"height":24}}]}}}' ;;
"terminal session")
  [ "$4" = "--help" ] && exit 0
  echo '{"type":"terminal.frame","seq":1,"full":true,"width":80,"height":24,"encoding":"ansi","bytes":"RlVMTA=="}'
  echo '{"type":"terminal.frame","seq":2,"full":false,"width":80,"height":24,"encoding":"ansi","bytes":"ZDE="}'
  echo '{"type":"terminal.frame","seq":3,"full":false,"width":80,"height":24,"encoding":"ansi","bytes":"ZDI="}'
  sleep 0.3 ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

type termEvent struct {
	Event     string `json:"-"`
	SessionID string `json:"session_id"`
	Epoch     string `json:"epoch"`
	Seq       int    `json:"seq"`
	Full      bool   `json:"full"`
	Cols      int    `json:"cols"`
	Data      string `json:"data"`
	Error     string `json:"error"`
}

func TestTerminalWatchStreamsFramesAndStartsOver(t *testing.T) {
	n := &Node{agents: map[string]herdr.Agent{"s1": {PaneID: "w1:p1"}}, procs: map[string]*assistantProc{}, state: newState(),
		herdr: &herdr.Client{Bin: fakeObserveHerdr(t)}}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	var mu sync.Mutex
	var got []termEvent
	hub.OnEvent = func(event string, params json.RawMessage) {
		var e termEvent
		_ = json.Unmarshal(params, &e)
		e.Event = event
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })

	res, err := n.terminalWatch(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if res["cols"] != 80 || res["rows"] != 24 {
		t.Fatalf("watch = %v", res)
	}
	// The fake's stream ends after three frames: the node observes again, a new epoch from a whole frame.
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		k := len(got)
		mu.Unlock()
		if k >= 4 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	n.terminalUnwatch("s1")
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 4 {
		t.Fatalf("events = %+v", got)
	}
	// The two diffs come right after the whole frame: they go out as one (termMinGap), "d1" + "d2".
	a, b := got[0], got[2]
	if a.Event != "terminal.frame" || !a.Full || a.Seq != 1 || a.Cols != 80 || a.Data != "RlVMTA==" ||
		got[1].Seq != 2 || got[1].Full || got[1].Data != base64.StdEncoding.EncodeToString([]byte("d1d2")) {
		t.Fatalf("first stream = %+v", got[:2])
	}
	if !b.Full || b.Seq != 1 || b.Epoch == a.Epoch {
		t.Fatalf("second stream = %+v (first epoch %s)", b, a.Epoch)
	}
}

func TestTerminalWatchNeedsARunningSession(t *testing.T) {
	n := &Node{agents: map[string]herdr.Agent{}, procs: map[string]*assistantProc{}, state: newState(),
		herdr: &herdr.Client{Bin: fakeObserveHerdr(t)}}
	if _, err := n.terminalWatch(context.Background(), "gone"); err == nil {
		t.Fatal("watched a session that is not running")
	}
}

func TestFrameGateMergesBurstsOnly(t *testing.T) {
	old := termMinGap
	termMinGap = 40 * time.Millisecond
	t.Cleanup(func() { termMinGap = old })
	var mu sync.Mutex
	var sent []herdr.Frame
	var at []time.Time
	g := &frameGate{send: func(f herdr.Frame) { mu.Lock(); sent = append(sent, f); at = append(at, time.Now()); mu.Unlock() }}
	b := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	start := time.Now()

	// After a quiet spell: at once. Then a burst: held, merged.
	g.put(herdr.Frame{Full: true, Data: b("F")})
	g.put(herdr.Frame{Data: b("a")})
	g.put(herdr.Frame{Data: b("b")})
	time.Sleep(80 * time.Millisecond)
	// Quiet again: at once. Then held, and a whole frame replaces the diffs before it; the last size wins.
	g.put(herdr.Frame{Data: b("c")})
	g.put(herdr.Frame{Data: b("d")})
	g.put(herdr.Frame{Full: true, Width: 90, Data: b("G")})
	g.put(herdr.Frame{Width: 91, Data: b("e")})
	g.flush() // the stream ended: what is held goes now

	mu.Lock()
	defer mu.Unlock()
	var got []string
	for _, f := range sent {
		d, _ := base64.StdEncoding.DecodeString(f.Data)
		got = append(got, fmt.Sprintf("%v:%s", f.Full, d))
	}
	if want := []string{"true:F", "false:ab", "false:c", "true:Ge"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent %v, want %v", got, want)
	}
	if at[0].Sub(start) > 10*time.Millisecond || at[2].Sub(start) < 80*time.Millisecond {
		t.Fatalf("first frames held: %v %v", at[0].Sub(start), at[2].Sub(start))
	}
	if gap := at[1].Sub(at[0]); gap < 35*time.Millisecond {
		t.Fatalf("burst went out %v after the frame before it", gap)
	}
	if sent[3].Width != 91 {
		t.Fatalf("merged frame keeps the last size: %+v", sent[3])
	}
}
