//go:build windows

package link

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPokeReachesWatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	if err := Poke(path, PokeCheck); err == nil {
		t.Fatal("poke with no watcher succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks, redials := make(chan struct{}, 4), make(chan struct{}, 4)
	if err := WatchPokes(ctx, path, func() { checks <- struct{}{} }, func() { redials <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind string
		ch   chan struct{}
	}{{PokeCheck, checks}, {PokeRedial, redials}} {
		if err := Poke(path, c.kind); err != nil {
			t.Fatal(err)
		}
		select {
		case <-c.ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s never arrived", c.kind)
		}
	}
	cancel()
	time.Sleep(100 * time.Millisecond) // the watcher closes its events
	if err := Poke(path, PokeCheck); err == nil {
		t.Fatal("poke after the watcher ended succeeded")
	}
}
