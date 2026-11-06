package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startingNoTranscript is a node with session s1 just started: known, but claude has not written its transcript.
// write() makes the transcript appear.
func startingNoTranscript(t *testing.T) (n *Node, write func()) {
	t.Helper()
	n, home := testNode(t)
	n.state.Sessions["s1"] = sessionState{}
	tp := filepath.Join(home, ".claude", "projects", "-src-app", "s1.jsonl")
	return n, func() {
		if err := os.MkdirAll(filepath.Dir(tp), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		n.mu.Lock()
		n.paths["s1"] = tp
		n.mu.Unlock()
	}
}

func watched(n *Node, id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	_, ok := n.watches[id]
	return ok
}

// A watch asked for before the transcript exists starts once it does.
func TestWatchBeforeTheTranscriptStartsWhenItAppears(t *testing.T) {
	n, write := startingNoTranscript(t)
	call, _ := servedNode(t, n)
	if err := call("session.watch", map[string]any{"session_id": "s1"}); err != nil {
		t.Fatal(err)
	}
	write()
	waitFor(t, "the watch", func() bool { return watched(n, "s1") })
	if err := call("session.unwatch", map[string]any{"session_id": "s1"}); err != nil {
		t.Fatal(err)
	}
}

// An unwatch while the watch waits for the transcript ends it: the session is not streamed once the file appears
// (#5389: the page was left right after the session started, and the node kept reading it for good).
func TestUnwatchBeforeTheTranscriptEndsTheWait(t *testing.T) {
	n, write := startingNoTranscript(t)
	call, _ := servedNode(t, n)
	for range 3 { // watched again meanwhile: still one wait
		if err := call("session.watch", map[string]any{"session_id": "s1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := call("session.unwatch", map[string]any{"session_id": "s1"}); err != nil {
		t.Fatal(err)
	}
	write()
	time.Sleep(1500 * time.Millisecond) // the wait looks every 500 ms
	if watched(n, "s1") {
		t.Fatal("the session is watched after the hub unwatched it")
	}
	n.mu.Lock()
	left := len(n.waiting)
	n.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d waits left", left)
	}
}
