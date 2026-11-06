package node

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/rpc"
)

func standbyHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "sb") // short: sun_path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	return home
}

// listenAt serves node.sock as a live node would (a `node --stdio` serving by itself): every request gets {"live":true}.
func listenAt(t *testing.T) net.Listener {
	t.Helper()
	path := nodeclient.SocketPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte(`{"live":true}` + "\n"))
			}()
		}
	}()
	return ln
}

func deadAt(t *testing.T) net.Listener {
	t.Helper()
	path := nodeclient.SocketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	return ln
}

func answeredBy(t *testing.T) string {
	t.Helper()
	line, err := nodeclient.Dial(context.Background(), sockRequest{Kind: "ping"})
	if err != nil {
		return "nobody"
	}
	var r map[string]any
	_ = json.Unmarshal(line, &r)
	switch {
	case r["live"] == true:
		return "live"
	case r["standby"] == true:
		return "standby"
	}
	return string(line)
}

// Standby leaves node.sock to a node that serves it, notices when one takes it over, and takes it back once it is gone.
func TestStandbyNeverTakesALiveSocket(t *testing.T) {
	standbyHome(t)
	live := listenAt(t)
	sb := newStandby(openSpool(SpoolPath()), t.Logf)
	sb.retry = 20 * time.Millisecond
	sb.want()
	defer sb.unwant()
	time.Sleep(100 * time.Millisecond)
	if sb.listening() || answeredBy(t) != "live" {
		t.Fatalf("standby took the socket of a live node: %s", answeredBy(t))
	}
	live.Close() // that node is gone (its file stays, nobody answers)
	waitFor(t, "standby to take node.sock", func() bool { return answeredBy(t) == "standby" })

	sb.unwant()
	dead := deadAt(t) // a listener that accepts and never answers (closing, or a stopped process)
	sb.want()
	waitFor(t, "standby to take a socket nobody answers on", func() bool { return answeredBy(t) == "standby" })
	dead.Close()

	other := listenAt(t) // a node serving by itself takes the path from standby
	defer other.Close()
	waitFor(t, "standby to let go of a path it no longer owns", func() bool { return !sb.listening() })
	if answeredBy(t) != "live" {
		t.Fatalf("answered by %s", answeredBy(t))
	}
	other.Close()
	waitFor(t, "standby to take it back", func() bool { return answeredBy(t) == "standby" })
	sb.unwant()
	time.Sleep(100 * time.Millisecond)
	if sb.listening() {
		t.Fatal("unwanted standby listens again")
	}
}

// The hold counts from the last connection that held, not from each standby start: a flapping manager does not keep
// every permission hook waiting.
func TestStandbyHoldDoesNotRenewOnFlaps(t *testing.T) {
	standbyHome(t)
	now := time.Now()
	sb := newStandby(openSpool(SpoolPath()), t.Logf)
	sb.now = func() time.Time { return now }
	sb.want()
	defer sb.unwant()
	if !sb.holding() {
		t.Fatal("a resident node starting holds")
	}
	for range 5 { // short connections come and go
		now = now.Add(3 * time.Minute)
		sb.unwant()
		sb.want()
	}
	if sb.holding() {
		t.Fatal("flaps renewed the hold")
	}
	sb.managerSeen(now) // a connection that held
	if !sb.holding() {
		t.Fatal("a connection that held renews it")
	}
}

// A notification the manager could not take stays (with what follows), and is dropped after spoolTriesMost tries.
func TestReplayKeepsWhatCouldNotBeHandedOver(t *testing.T) {
	n, _ := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	n.panes = map[string]string{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	refuse := true // read by the hub's handler goroutine only after the writes below are done (rpc round trips)
	var mu sync.Mutex
	var events []string
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(events) }
	busy := false
	hub.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "notify" && busy {
			return nil, &rpc.Error{Code: spoolBusy, Message: "still being sent"}
		}
		if method == "notify" && refuse {
			return nil, &rpc.Error{Code: "busy", Message: "try later"}
		}
		return map[string]any{"sent": true}, nil
	}
	hub.OnEvent = func(event string, params json.RawMessage) {
		var p struct {
			Event string `json:"event"`
		}
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		events = append(events, p.Event)
		mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { _ = n2hW.Close(); _ = h2nW.Close() })

	sp := openSpool(filepath.Join(t.TempDir(), spoolFile))
	_, _ = sp.add(spoolEntry{ID: "n1", At: time.Now(), Kind: "notify", Request: sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":"t"}`)}})
	_, _ = sp.add(spoolEntry{ID: "h1", At: time.Now(), Kind: "hook", Request: hookReq("Stop", "", nil)})
	replay := func() {
		n.replayed, n.stateReady = make(chan struct{}), make(chan struct{})
		close(n.stateReady)
		n.replaySpool(ctx, sp)
	}
	busy = true // the manager still has an earlier try: kept, not counted
	replay()
	if got := sp.load(); len(got) != 1 || got[0].ID != "n1" || got[0].Tries != 0 {
		t.Fatalf("after a busy answer: %+v, want the notify kept untried (the Stop after it went on)", got)
	}
	busy = false
	replay()
	if got := sp.load(); len(got) != 1 || got[0].ID != "n1" || got[0].Tries != 1 {
		t.Fatalf("after a refused notify: %+v, want it kept, tried once", got)
	}
	waitFor(t, "the Stop after it to go on (hook events keep their order)", func() bool { return count() == 1 })
	replay()
	replay() // the third refusal drops it
	if got := sp.load(); len(got) != 0 {
		t.Fatalf("left: %+v", got)
	}
	refuse = false
	_, _ = sp.add(spoolEntry{ID: "n2", At: time.Now(), Kind: "notify", Request: sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":"t"}`)}})
	cancel() // the connection is gone: nothing is counted against it
	n.replayed, n.stateReady = make(chan struct{}), make(chan struct{})
	close(n.stateReady)
	n.replaySpool(ctx, sp)
	if got := sp.load(); len(got) != 1 || got[0].Tries != 0 {
		t.Fatalf("a replay on a gone connection: %+v", got)
	}
}

// Replay waits for the session state (takesReport needs it), and only a resident node's connection replays at all.
func TestReplayWaitsForTheSessionState(t *testing.T) {
	n, _ := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	rec := newReplayHub(t, n)
	sp := openSpool(filepath.Join(t.TempDir(), spoolFile))
	_, _ = sp.add(spoolEntry{ID: "h1", At: time.Now(), Kind: "hook", Request: hookReq("Stop", "", func(r *sockRequest) { r.ReportOnly = true })})
	n.replayed, n.stateReady = make(chan struct{}), make(chan struct{})
	n.state.Sessions[reportSession] = sessionState{} // Persona's own session: its report-only events are not taken
	go n.replaySpool(context.Background(), sp)
	time.Sleep(100 * time.Millisecond)
	select {
	case <-n.replayed:
		t.Fatal("replayed before the state came")
	default:
	}
	close(n.stateReady)
	<-n.replayed
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.got) != 0 {
		t.Fatalf("a report-only event of Persona's own session was reported: %v", rec.got)
	}
}

// A manager that does not answer a notification in time: that one counts a try, the other notifications wait (untried)
// for the next connection, and the hook events go on in order. A connection that drops counts no try.
func TestReplayTimeoutsAndDrops(t *testing.T) {
	defer func(d time.Duration) { replayNotifyTimeout = d }(replayNotifyTimeout)
	replayNotifyTimeout = 50 * time.Millisecond
	n, _ := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	n.panes = map[string]string{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	var mu sync.Mutex
	var events []string
	hub.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		<-ctx.Done() // never answers
		return nil, ctx.Err()
	}
	hub.OnEvent = func(event string, _ json.RawMessage) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	sp := openSpool(filepath.Join(t.TempDir(), spoolFile))
	note := func(id string) spoolEntry {
		return spoolEntry{ID: id, At: time.Now(), Kind: "notify", Request: sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":"t"}`)}}
	}
	_, _ = sp.add(note("n1"))
	_, _ = sp.add(note("n2"))
	_, _ = sp.add(spoolEntry{ID: "h1", At: time.Now(), Kind: "hook", Request: hookReq("Stop", "", nil)})
	n.replayed, n.stateReady = make(chan struct{}), make(chan struct{})
	close(n.stateReady)
	start := time.Now()
	n.replaySpool(ctx, sp)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the replay took %s: one timeout should be all it waits", took)
	}
	got := sp.load()
	if len(got) != 2 || got[0].ID != "n1" || got[0].Tries != 1 || got[1].ID != "n2" || got[1].Tries != 0 {
		t.Fatalf("kept %+v: n1 tried once, n2 untried", got)
	}
	waitFor(t, "the Stop to go on", func() bool { mu.Lock(); defer mu.Unlock(); return len(events) == 1 })

	_ = h2nW.Close() // the connection drops: a call fails without ctx ending
	_ = n2hW.Close()
	n.replayed = make(chan struct{})
	n.replaySpool(ctx, sp)
	if got := sp.load(); len(got) != 2 || got[0].Tries != 1 || got[1].Tries != 0 {
		t.Fatalf("a dropped connection counted a try: %+v", got)
	}
}
