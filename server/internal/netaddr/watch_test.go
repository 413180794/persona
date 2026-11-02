package netaddr

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func receiveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("channel closed early")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("missing refresh")
	}
}

func TestWatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := Watch(ctx)
	receiveSignal(t, ch)
	cancel()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline.C:
			t.Fatal("watch did not close")
		}
	}
}

func TestWatchAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case _, ok := <-Watch(ctx):
		if ok {
			t.Fatal("cancelled watch emitted signal")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled watch not closed")
	}
}

func TestWatchCoalescesAndDoesNotBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan struct{}, 1)
	events := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWatch(ctx, out, events, nil, func() ([]Addr, error) { return nil, nil }, 20*time.Millisecond)
	}()
	receiveSignal(t, out)
	for i := 0; i < 20; i++ {
		select {
		case events <- struct{}{}:
		case <-time.After(time.Second):
			t.Fatal("blocked event producer")
		}
	}
	receiveSignal(t, out)
	select {
	case <-out:
		t.Fatal("burst was not coalesced")
	case <-time.After(50 * time.Millisecond):
	}
	// Leave a signal unread, then cancel with more work pending.
	events <- struct{}{}
	time.Sleep(30 * time.Millisecond)
	events <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unread output blocked shutdown")
	}
}

func TestWatchPollingAndRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	events := make(chan struct{})
	out := make(chan struct{}, 1)
	calls := make(chan struct{}, 1)
	var mu sync.Mutex
	a := stableAddr(1)
	snapshot := []Addr{a}
	var listErr error
	list := func() ([]Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		defer func() { calls <- struct{}{} }()
		return append([]Addr(nil), snapshot...), listErr
	}
	done := make(chan struct{})
	go func() { defer close(done); runWatch(ctx, out, events, ticks, list, time.Millisecond) }()
	receiveSignal(t, out)
	receiveSignal(t, calls)
	poll := func() {
		t.Helper()
		select {
		case ticks <- time.Time{}:
		case <-time.After(time.Second):
			t.Fatal("poll blocked")
		}
		receiveSignal(t, calls)
	}
	poll()
	select {
	case <-out:
		t.Fatal("unchanged snapshot signalled")
	case <-time.After(20 * time.Millisecond):
	}
	// A failed native subscription must leave polling active.
	close(events)
	mu.Lock()
	snapshot[0].Flags = FlagDeprecated
	mu.Unlock()
	poll()
	receiveSignal(t, out)
	mu.Lock()
	listErr = errors.New("temporary read failure")
	mu.Unlock()
	poll()
	receiveSignal(t, out)
	mu.Lock()
	listErr = nil
	snapshot = nil
	mu.Unlock()
	poll()
	receiveSignal(t, out)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poller did not stop")
	}
}

func TestSnapshotComparison(t *testing.T) {
	a, b := stableAddr(1), stableAddr(2)
	original := []Addr{a, b}
	newer := []Addr{b, a}
	newer[0].PreferredLifetime--
	newer[0].ValidLifetime--
	if !reflect.DeepEqual(comparableSnapshot(original), comparableSnapshot(newer)) {
		t.Fatal("ordering/countdown produced false change")
	}
	if original[0] != a || original[1] != b {
		t.Fatal("mutated input")
	}
	changes := []func(*Addr){
		func(a *Addr) { a.PreferredLifetime = 0 },
		func(a *Addr) { a.ValidLifetime = 0 },
		func(a *Addr) { a.PreferredLifetime = InfiniteLifetime },
		func(a *Addr) { a.Flags = FlagDeprecated },
		func(a *Addr) { a.InterfaceFlags &^= net.FlagUp },
		func(a *Addr) { a.Virtual = true },
		func(a *Addr) { a.PrefixLen = 128 },
	}
	for _, change := range changes {
		changed := a
		change(&changed)
		if reflect.DeepEqual(comparableSnapshot([]Addr{a}), comparableSnapshot([]Addr{changed})) {
			t.Fatal("lost meaningful change")
		}
	}
}

func TestAddressChangeMessages(t *testing.T) {
	tests := []struct {
		name, hex string
		want      bool
	}{
		{"new v6", ipv6Wire, true},
		{"new v4", ipv4Wire, true},
		{"delete", `10000000 1500 0000 00000000 00000000`, true},
		{"link down", `10000000 1000 0000 00000000 00000000`, true},
		{"overrun", `10000000 0400 0000 00000000 00000000`, true},
		{"error", `14000000 0200 0000 01000000 00000000 ffffffff`, true},
		{"ack", `14000000 0200 0000 01000000 00000000 00000000`, false},
		{"done", doneWire, false},
		{"malformed", "01", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if addressChange(wire(t, tt.hex), binary.LittleEndian) != tt.want {
				t.Fatal("wrong change detection")
			}
		})
	}
}

func TestListLocal(t *testing.T) {
	addrs, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		if !a.IP.IsValid() || a.Interface == "" || a.InterfaceIndex <= 0 || a.PrefixLen < 0 || a.PrefixLen > a.IP.BitLen() || a.Class != Classify(a.IP) {
			t.Fatal("invalid local snapshot")
		}
		if runtime.GOOS != "linux" && (a.FlagsKnown || a.LifetimesKnown || a.ScopeKnown || Stable(a)) {
			t.Fatal("fallback invented metadata")
		}
		if runtime.GOOS == "linux" && (!a.FlagsKnown || !a.ScopeKnown) {
			t.Fatal("missing Linux metadata")
		}
	}
}
