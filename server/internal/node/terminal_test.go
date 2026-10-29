package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	Control   bool   `json:"control"`
	Taken     bool   `json:"taken"`
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

	res, err := n.terminalWatch(ctx, "s1", nil)
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
	if _, err := n.terminalWatch(context.Background(), "gone", nil); err == nil {
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

// fakeControlHerdr is herdr's terminal session control: a whole frame at the size asked for, then what comes in on
// stdin goes to log (a resize answers with a whole frame, "TAKE" plays someone taking the slot), and stdin closing is
// the release. With busy present, a control without --takeover finds the slot held.
func fakeControlHerdr(t *testing.T) (bin, log, busy string) {
	t.Helper()
	dir := t.TempDir()
	bin, log, busy = filepath.Join(dir, "herdr"), filepath.Join(dir, "log"), filepath.Join(dir, "busy")
	script := fmt.Sprintf(`#!/bin/sh
case "$1 $2" in
"terminal session")
  [ "$4" = "--help" ] && exit 0
  case "$*" in *--takeover*) ;; *) if [ -f %[2]q ]; then
    echo '{"type":"terminal.closed","reason":"terminal attach failed: terminal t1 already has an attached client; retry with --takeover"}'; exit 0; fi ;; esac
  echo "start $*" >> %[1]q
  echo '{"type":"terminal.frame","seq":1,"full":true,"width":44,"height":30,"encoding":"ansi","bytes":"RlVMTA=="}'
  while read -r line; do
    printf "%%s\n" "$line" >> %[1]q
    case "$line" in
    *terminal.resize*) echo '{"type":"terminal.frame","seq":2,"full":true,"width":40,"height":20,"encoding":"ansi","bytes":"RlVMTA=="}' ;;
    *TAKE*) echo '{"type":"terminal.closed","reason":"terminal attach taken over"}'; exit 0 ;;
    esac
  done
  echo released >> %[1]q
  echo '{"type":"terminal.closed","reason":"detached"}' ;;
*) exit 1 ;;
esac
`, log, busy)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log, busy
}

// controlNode is a node on a fake herdr, wired to a hub peer whose events are collected.
func controlNode(t *testing.T, bin string) (*Node, *rpc.Peer, func() []termEvent) {
	t.Helper()
	controlSupport.once.Do(func() { controlSupport.ok = true })
	n := &Node{agents: map[string]herdr.Agent{"s1": {PaneID: "w1:p1"}}, procs: map[string]*assistantProc{}, state: newState(),
		herdr: &herdr.Client{Bin: bin}}
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	n.peer.OnEvent = n.onTerminalEvent
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
	t.Cleanup(func() {
		if s := n.termStreamOf("s1"); s != nil && s.ctl != nil {
			n.terminalUnwatch("s1")
			select { // the fake writes its log until it goes
			case <-s.ctl.Done():
			case <-time.After(5 * time.Second):
			}
		}
		cancel()
		_ = n2hW.Close()
		_ = h2nW.Close()
	})
	return n, hub, func() []termEvent { mu.Lock(); defer mu.Unlock(); return append([]termEvent(nil), got...) }
}

func readLog(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestTerminalControlTypesResizesAndLetsGo(t *testing.T) {
	bin, log, _ := fakeControlHerdr(t)
	n, hub, events := controlNode(t, bin)
	res, err := n.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	if res["control"] != true || res["cols"] != 44 || res["rows"] != 30 {
		t.Fatalf("watch = %v", res)
	}
	if l := readLog(log); !strings.Contains(l, "control w1:p1 --cols 44 --rows 30") || strings.Contains(l, "--takeover") {
		t.Fatalf("herdr ran %q", l)
	}
	_ = hub.Notify("terminal.input", map[string]any{"session_id": "s1", "text": "ab\r"})
	_ = hub.Notify("terminal.resize", map[string]any{"session_id": "s1", "cols": 40, "rows": 20})
	waitFor(t, "the resized frame", func() bool {
		for _, e := range events() {
			if e.Event == "terminal.frame" && e.Full && e.Cols == 40 {
				return true
			}
		}
		return false
	})
	l := readLog(log)
	if !strings.Contains(l, `{"text":"ab\r","type":"terminal.input"}`) || !strings.Contains(l, `{"cols":40,"rows":20,"type":"terminal.resize"}`) {
		t.Fatalf("herdr got %q", l)
	}
	n.terminalUnwatch("s1")
	waitFor(t, "the release", func() bool { return strings.Contains(readLog(log), "released") })
	for _, e := range events() {
		if e.Event == "terminal.end" {
			t.Fatalf("letting go on the hub's word is not an end to report: %+v", e)
		}
	}
}

func TestTerminalControlAsksBeforeTakingOver(t *testing.T) {
	bin, log, busy := fakeControlHerdr(t)
	if err := os.WriteFile(busy, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	n, _, _ := controlNode(t, bin)
	_, err := n.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "attach_busy" {
		t.Fatalf("busy slot: %v", err)
	}
	if _, err := n.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30, Takeover: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readLog(log), "--takeover") {
		t.Fatal("did not take over")
	}
}

func TestTerminalControlEndsWhenTakenOrForgotten(t *testing.T) {
	bin, _, _ := fakeControlHerdr(t)
	n, hub, events := controlNode(t, bin)
	if _, err := n.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	_ = hub.Notify("terminal.input", map[string]any{"session_id": "s1", "text": "TAKE"})
	ended := func() *termEvent {
		for _, e := range events() {
			if e.Event == "terminal.end" {
				return &e
			}
		}
		return nil
	}
	waitFor(t, "terminal.end", func() bool { return ended() != nil })
	if e := ended(); !e.Taken || !e.Control {
		t.Fatalf("end = %+v", e)
	}

	old := termLease
	termLease = 100 * time.Millisecond
	t.Cleanup(func() { termLease = old })
	n2, _, events2 := controlNode(t, bin)
	if _, err := n2.terminalWatch(context.Background(), "s1", &termControl{Cols: 44, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the lease to run out", func() bool {
		for _, e := range events2() {
			if e.Event == "terminal.end" && e.Control && !e.Taken {
				return true
			}
		}
		return false
	})
}
