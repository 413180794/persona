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
