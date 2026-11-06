package assistant

import (
	"context"
	"testing"
	"time"
)

// stuckHealthStore is a database whose launcher health setting never answers a write.
type stuckHealthStore struct{ *fleetMem }

func (s stuckHealthStore) PutSetting(ctx context.Context, key string, v any) error {
	if key == launchersHealthKey {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.fleetMem.PutSetting(ctx, key, v)
}

// A launcher health save that the database does not answer gives up at historyTimeout: the health lock, which every
// start and readiness check takes, is not held for good (#5467).
func TestLauncherHealthSaveIsBounded(t *testing.T) {
	shortHistoryTimeout(t, 100*time.Millisecond)
	ft := newFleetTestOn(t, stuckHealthStore{newFleetMem()})
	s := ft.s
	done := make(chan LauncherHealth, 1)
	go func() {
		done <- s.recordHealth(context.Background(), "mac", "claude", healthResult{ok: true, source: "check"})
	}()
	select {
	case r := <-done:
		if r.State != healthOK {
			t.Fatalf("record = %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recording a launcher's health waits for the database for good")
	}
	got := make(chan bool, 1)
	go func() { _, ok := s.launcherHealth(context.Background(), "mac", "claude"); got <- ok }()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("the record is not kept in memory when it could not be saved")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading launcher health waits behind the stuck save")
	}
}
