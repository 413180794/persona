package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
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

// fakeSlotHerdr is one computer's herdr with panes w1:p1 and w1:p2. An observe sends a whole frame and runs until it
// is killed; a control holds the pane's attach slot (a file) until its stdin closes, and finds it busy when it is
// held, unless --takeover. Every observe and control writes its pid to log.
func fakeSlotHerdr(t *testing.T) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "herdr"), filepath.Join(dir, "log")
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
"pane layout") echo '{"result":{"layout":{"panes":[{"pane_id":"w1:p1","rect":{"width":80,"height":24}},{"pane_id":"w1:p2","rect":{"width":80,"height":24}}]}}}' ;;
"terminal session")
  [ "$4" = "--help" ] && exit 0
  slot=%[2]q/"slot-$4"
  case "$3" in
  observe)
    echo '{"type":"terminal.frame","seq":1,"full":true,"width":80,"height":24,"encoding":"ansi","bytes":"RlVMTA=="}'
    echo "observe $$" >> %[1]q
    exec sleep 30 ;;
  control)
    case "$*" in *--takeover*) ;; *) if [ -f "$slot" ]; then
      echo '{"type":"terminal.closed","reason":"terminal attach failed: terminal t1 already has an attached client; retry with --takeover"}'; exit 0; fi ;; esac
    : > "$slot"
    echo "control $$" >> %[1]q
    echo '{"type":"terminal.frame","seq":1,"full":true,"width":44,"height":30,"encoding":"ansi","bytes":"RlVMTA=="}'
    while read -r line; do :; done
    rm -f "$slot"
    echo '{"type":"terminal.closed","reason":"detached"}' ;;
  esac ;;
*) exit 1 ;;
esac
`, log, dir)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// slotNode is a node on the computer whose herdr is bin: session s1 runs in pane w1:p1, s2 in w1:p2.
func slotNode(bin string) *Node {
	controlSupport.once.Do(func() { controlSupport.ok = true })
	return &Node{agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}, "s2": {PaneID: "w1:p2"}},
		procs: map[string]*assistantProc{}, state: newState(), hosts: herdrHosts(&herdr.Client{Bin: bin})}
}

// hubConn is one hub connection to a node, served as Node.Serve serves it: the connection's ctx ends once the node
// stops reading (the hub went away). The node's side runs under the pprof label conn=<name>, which every goroutine a
// request starts inherits: what is left of a connection is the goroutines still carrying its label.
type hubConn struct {
	name   string
	hub    *rpc.Peer
	ctx    context.Context
	h2nW   *io.PipeWriter
	served chan struct{}
}

func connect(t *testing.T, name string, n *Node) *hubConn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	n.peer.OnRequest = n.handle
	n.peer.OnEvent = n.onTerminalEvent
	peer := n.peer
	c := &hubConn{name: name, hub: rpc.New("h", h2nW), ctx: ctx, h2nW: h2nW, served: make(chan struct{})}
	go func() { _ = c.hub.Serve(ctx, n2hR) }()
	go func() {
		defer close(c.served)
		pprof.Do(context.Background(), pprof.Labels("conn", name), func(lctx context.Context) {
			sctx, scancel := context.WithCancel(lctx)
			defer scancel() // Node.Serve: the connection's ctx ends when it stops reading
			_ = peer.Serve(sctx, h2nR)
		})
	}()
	t.Cleanup(func() {
		c.drop(t)
		_ = n2hW.Close()
		cancel()
	})
	return c
}

// drop ends the connection from the hub's side, and waits until the node stopped reading it.
func (c *hubConn) drop(t *testing.T) {
	t.Helper()
	_ = c.h2nW.Close()
	select {
	case <-c.served:
	case <-time.After(5 * time.Second):
		t.Fatalf("connection %s: the node kept reading", c.name)
	}
}

func (c *hubConn) watch(id string, control bool) error {
	params := map[string]any{"session_id": id}
	if control {
		params["control"] = map[string]any{"cols": 44, "rows": 30}
	}
	var res map[string]any
	if err := c.hub.Call(c.ctx, "terminal.watch", params, &res); err != nil {
		return err
	}
	if res["control"] != control {
		return fmt.Errorf("watch %s = %v", id, res)
	}
	return nil
}

// goroutinesOf counts the goroutines carrying conn's label; with only, those running one of those functions.
func goroutinesOf(conn string, only ...string) (int, string) {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	total, stacks := 0, ""
	for _, rec := range strings.Split(b.String(), "\n\n") {
		if !strings.Contains(rec, `"conn":"`+conn+`"`) {
			continue
		}
		match := len(only) == 0
		for _, f := range only {
			match = match || strings.Contains(rec, f)
		}
		if !match {
			continue
		}
		k := 0
		_, _ = fmt.Sscanf(rec, "%d @", &k)
		total += k
		stacks += rec + "\n\n"
	}
	return total, stacks
}

var streamLoops = []string{"(*Node).terminalLoop+", "(*Node).controlLoop+"}

// herdrProcs are the pids of the observes and controls a fake herdr started, by kind.
func herdrProcs(t *testing.T, log string) map[string][]int {
	t.Helper()
	b, _ := os.ReadFile(log)
	procs := map[string][]int{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if kind, pid, ok := strings.Cut(l, " "); ok {
			if p, err := strconv.Atoi(pid); err == nil {
				procs[kind] = append(procs[kind], p)
			}
		}
	}
	return procs
}

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

func countAlive(pids []int) int {
	k := 0
	for _, p := range pids {
		if pidAlive(p) {
			k++
		}
	}
	return k
}

// killLeft ends the fake herdrs a failing run leaves behind, before their directory is removed.
func killLeft(t *testing.T, logs ...string) {
	t.Cleanup(func() {
		for _, log := range logs {
			for _, pids := range herdrProcs(t, log) {
				for _, p := range pids {
					if pr, err := os.FindProcess(p); err == nil {
						_ = pr.Kill()
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	})
}

// drives starts an observe of s1 and a control of s2 on c, and waits until its herdr runs both and the two streams
// outlive the requests that started them.
func (c *hubConn) drives(t *testing.T, log string) {
	t.Helper()
	if err := c.watch("s1", false); err != nil {
		t.Fatalf("%s observe: %v", c.name, err)
	}
	if err := c.watch("s2", true); err != nil {
		t.Fatalf("%s control: %v", c.name, err)
	}
	waitFor(t, c.name+"'s herdr processes", func() bool {
		p := herdrProcs(t, log)
		return countAlive(p["observe"]) == 1 && countAlive(p["control"]) == 1
	})
	waitFor(t, c.name+"'s streams", func() bool { k, _ := goroutinesOf(c.name, streamLoops...); return k == 2 })
}

// ended waits until nothing of connection c runs any more: no goroutine of it, no herdr process it started, and its
// attach slot free.
func (c *hubConn) ended(t *testing.T, log string) {
	t.Helper()
	waitFor(t, c.name+"'s herdr processes to end", func() bool {
		p := herdrProcs(t, log)
		return countAlive(p["observe"])+countAlive(p["control"]) == 0
	})
	waitFor(t, "every goroutine of connection "+c.name+" to end", func() bool { k, _ := goroutinesOf(c.name); return k == 0 })
	if _, err := os.Stat(filepath.Join(filepath.Dir(log), "slot-w1:p2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s's attach slot is still held: %v", c.name, err)
	}
}

// Two computers' resident nodes, each on its own hub connection: when one connection ends, only what it started
// stops; the other's observe and control go on, its herdr processes and goroutines untouched (#5331).
func TestTerminalStreamsOfAnotherConnectionGoOn(t *testing.T) {
	binA, logA := fakeSlotHerdr(t)
	binB, logB := fakeSlotHerdr(t)
	killLeft(t, logA, logB)
	nB := slotNode(binB)
	a, b := connect(t, "A", slotNode(binA)), connect(t, "B", nB)
	a.drives(t, logA)
	b.drives(t, logB)

	a.drop(t)
	a.ended(t, logA)
	time.Sleep(300 * time.Millisecond) // anything A's end would take down with it has had the time to
	if k, stacks := goroutinesOf("B", streamLoops...); k != 2 {
		t.Fatalf("connection B streams after A ended: %d goroutines, want 2\n%s", k, stacks)
	}
	if p := herdrProcs(t, logB); countAlive(p["observe"]) != 1 || countAlive(p["control"]) != 1 || len(p["observe"]) != 1 {
		t.Fatalf("B's herdr processes after A ended: %v", p)
	}
	if nB.termStreamOf("s1") == nil || nB.termStreamOf("s2") == nil {
		t.Fatal("B's node forgot its streams")
	}

	b.drop(t)
	b.ended(t, logB)
}

// A resident node reconnects (a new Node per connection, as Attach makes): the phone takes the terminal again at its
// size without a takeover, since the last connection's control gave the attach slot back, and the observe starts
// again; nothing of the last connection is left running beside the new one (#5331).
func TestTerminalStreamsStartAgainAfterReconnecting(t *testing.T) {
	bin, log := fakeSlotHerdr(t)
	killLeft(t, log)
	a := connect(t, "A", slotNode(bin))
	a.drives(t, log)
	a.drop(t)
	a.ended(t, log)

	a2 := connect(t, "A2", slotNode(bin))
	if err := a2.watch("s2", true); err != nil {
		t.Fatalf("control after reconnecting: %v", err)
	}
	if err := a2.watch("s1", false); err != nil {
		t.Fatalf("observe after reconnecting: %v", err)
	}
	waitFor(t, "A2's streams", func() bool { k, _ := goroutinesOf("A2", streamLoops...); return k == 2 })
	waitFor(t, "one observe and one control after reconnecting", func() bool {
		p := herdrProcs(t, log)
		return countAlive(p["observe"]) == 1 && countAlive(p["control"]) == 1 && len(p["observe"]) == 2 && len(p["control"]) == 2
	})
	if k, stacks := goroutinesOf("A"); k != 0 {
		t.Fatalf("connection A still runs %d goroutines beside A2\n%s", k, stacks)
	}

	a2.drop(t)
	a2.ended(t, log)
}
