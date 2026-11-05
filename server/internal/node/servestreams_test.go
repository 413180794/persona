package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A watch belongs to the hub connection that asked for it. The resident node makes one Node per connection, so a
// watch left running from the one before keeps reading its transcript four times a second for a peer that is gone,
// and the Node and the files it parsed stay in memory until the process restarts.
func TestWatchStopsWithItsConnection(t *testing.T) {
	n, home := testNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	writeCodexToolLines(t, path, 0, 1)
	hub := withHub(t, n)

	ctx, cancel := context.WithCancel(context.Background())
	if err := n.watchFor(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	writeCodexToolLines(t, path, 1, 1) // a line after the watch began is streamed
	waitFor(t, "the first push", func() bool { return len(hub.got()) >= 2 })

	cancel() // the connection ends
	waitFor(t, "the watch to stop", func() bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		_, ok := n.watches[id]
		return !ok
	})

	before := len(hub.got())
	writeCodexToolLines(t, path, 2, 20)
	time.Sleep(700 * time.Millisecond) // several 250 ms reads
	if after := len(hub.got()); after != before {
		t.Fatalf("the watch kept reading after its connection ended: items %d -> %d", before, after)
	}
}

// An unwatch followed at once by a watch (transcriptMoved, or the hub switching pages fast) registers a new watch
// before the old loop has seen its cancel. The old loop must not then drop the new entry on its way out: the session
// would look unwatched, refresh would stop pushing, and the hub, still subscribed, would never notice.
func TestOldWatchLoopKeepsNewWatch(t *testing.T) {
	n, home := testNode(t)
	const id = "22222222-3333-4444-5555-666666666666"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	writeCodexToolLines(t, path, 0, 1)
	hub := withHub(t, n)

	if err := n.watch(id, 0); err != nil {
		t.Fatal(err)
	}
	current := func() *watch {
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.watches[id]
	}
	old := current()

	// Park the old loop inside refresh, waiting on the push lock.
	mu := n.pushLock(id)
	mu.Lock()
	time.Sleep(400 * time.Millisecond) // more than one 250 ms tick
	n.unwatch(id)
	done := make(chan error, 1)
	go func() { done <- n.watch(id, 0) }() // registers, then waits on the push lock too
	waitFor(t, "the new watch to register", func() bool { w := current(); return w != nil && w != old })
	mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // the old loop leaves through ctx.Done

	if current() == nil {
		t.Fatal("the old watch loop dropped the new watch")
	}
	before := len(hub.got())
	writeCodexToolLines(t, path, 1, 1)
	waitFor(t, "the new watch to push", func() bool { return len(hub.got()) > before })
	n.unwatch(id)
}
