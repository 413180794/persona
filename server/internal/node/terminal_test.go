package node

import (
	"context"
	"encoding/json"
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
		if k >= 6 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	n.terminalUnwatch("s1")
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 6 {
		t.Fatalf("events = %+v", got)
	}
	a, b := got[0], got[3]
	if a.Event != "terminal.frame" || !a.Full || a.Seq != 1 || a.Cols != 80 || a.Data != "RlVMTA==" || got[2].Seq != 3 || got[2].Full {
		t.Fatalf("first stream = %+v", got[:3])
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
