package node

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// servedTerminalNode serves n over a real rpc.Peer pair, as Node.Serve does for one hub connection: requests carry
// the connection's context, and end() is that connection going away (#5331).
func servedTerminalNode(t *testing.T, n *Node) (call func(method string, params any) error, end func()) {
	t.Helper()
	hubCtx, hubCancel := context.WithCancel(context.Background())
	connCtx, connCancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	n.peer.OnRequest = n.handle
	n.peer.OnEvent = n.onTerminalEvent
	hub := rpc.New("h", h2nW)
	go func() { _ = hub.Serve(hubCtx, n2hR) }()
	go func() { _ = n.peer.Serve(connCtx, h2nR) }()
	end = func() { connCancel(); _ = h2nW.Close() }
	t.Cleanup(func() { end(); hubCancel(); _ = n2hW.Close() })
	return func(method string, params any) error { return hub.Call(hubCtx, method, params, nil) }, end
}

// A controlled terminal is let go (the slot freed, the pane back to its own size) when the hub connection that took
// it ends: a resident node's next connection is a new Node, which could not take it before the lease ran out.
func TestTerminalControlEndsWithTheConnection(t *testing.T) {
	bin, log, _ := fakeControlHerdr(t)
	controlSupport.once.Do(func() { controlSupport.ok = true })
	n := &Node{agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}}, procs: map[string]*assistantProc{}, state: newState(),
		hosts: herdrHosts(&herdr.Client{Bin: bin})}
	call, end := servedTerminalNode(t, n)
	if err := call("terminal.watch", map[string]any{"session_id": "s1", "control": map[string]any{"cols": 44, "rows": 30}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the control to start", func() bool { return strings.Contains(readLog(log), "start ") })
	end()
	waitFor(t, "the control to be let go", func() bool { return strings.Contains(readLog(log), "released") })
}

// An observed terminal stops observing when the hub connection that asked for it ends, instead of starting herdr's
// observe over and over on a Node nobody talks to any more.
func TestTerminalWatchEndsWithTheConnection(t *testing.T) {
	dir := t.TempDir()
	bin, starts := filepath.Join(dir, "herdr"), filepath.Join(dir, "starts")
	script := `#!/bin/sh
case "$1 $2" in
"pane layout") echo '{"result":{"layout":{"panes":[{"pane_id":"w1:p1","rect":{"width":80,"height":24}}]}}}' ;;
"terminal session")
  [ "$4" = "--help" ] && exit 0
  echo start >> ` + starts + `
  echo '{"type":"terminal.frame","seq":1,"full":true,"width":80,"height":24,"encoding":"ansi","bytes":"RlVMTA=="}'
  sleep 0.2 ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	n := &Node{agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}}, procs: map[string]*assistantProc{}, state: newState(),
		hosts: herdrHosts(&herdr.Client{Bin: bin})}
	call, end := servedTerminalNode(t, n)
	if err := call("terminal.watch", map[string]any{"session_id": "s1"}); err != nil {
		t.Fatal(err)
	}
	count := func() int { return strings.Count(readLog(starts), "start") }
	waitFor(t, "observe to start over once", func() bool { return count() >= 2 })
	end()
	time.Sleep(500 * time.Millisecond) // an observe under way when the connection ended may still log its start
	before := count()
	time.Sleep(1500 * time.Millisecond) // an epoch here is ~0.3 s: a stream still running starts several more
	if after := count(); after != before {
		t.Fatalf("observe started %d more times after the connection ended", after-before)
	}
}
