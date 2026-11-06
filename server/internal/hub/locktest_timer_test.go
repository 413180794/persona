package hub

import (
	"context"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// lockTestCountingStore keeps scheduled tests in memory and counts the dispatcher's queries.
type lockTestCountingStore struct {
	sharedStore
	mu        sync.Mutex
	tests     []db.LockTest
	due, next int
	attempted map[string]time.Time
}

func (s *lockTestCountingStore) RecoverLockTests(context.Context, time.Time) error { return nil }

func (s *lockTestCountingStore) NextLockTestDue(context.Context) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	var next *time.Time
	for _, t := range s.tests {
		if t.Status == "scheduled" && (next == nil || t.DueAt.Before(*next)) {
			due := t.DueAt
			next = &due
		}
	}
	return next, nil
}

func (s *lockTestCountingStore) DueLockTests(_ context.Context, at time.Time) ([]db.LockTest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.due++
	var out []db.LockTest
	for _, t := range s.tests {
		if t.Status == "scheduled" && !t.DueAt.After(at) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *lockTestCountingStore) AttemptLockTest(_ context.Context, testID string, _ int64, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tests {
		if t.TestID == testID && t.Status == "scheduled" {
			s.tests[i].Status = "unavailable"
			s.attempted[testID] = at
			return true, nil
		}
	}
	return false, nil
}

func (s *lockTestCountingStore) counts() (due, next int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.due, s.next
}

func TestLockTestDispatcherSleepsWhenIdleAndWakesOnSchedule(t *testing.T) {
	store := &lockTestCountingStore{attempted: map[string]time.Time{}}
	h := &Hub{store: store, clients: map[*client]struct{}{}, logger: log.New(io.Discard, "", 0)}
	stop, err := h.startLockTests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	// Idle: one lookup of the next due time, then nothing until a wake or the recheck.
	time.Sleep(3 * time.Second)
	if due, next := store.counts(); due != 0 || next != 1 {
		t.Fatalf("idle dispatcher queried the store: DueLockTests=%d NextLockTestDue=%d", due, next)
	}

	// Scheduling a test (as serveLockTest does) re-arms the timer for its due time.
	dueAt := time.Now().Add(time.Second)
	store.mu.Lock()
	store.tests = append(store.tests, db.LockTest{TestID: "lt_timer", DeviceID: "d", DueAt: dueAt, Status: "scheduled"})
	store.mu.Unlock()
	h.lockTests.wake()
	deadline := time.Now().Add(5 * time.Second)
	for {
		store.mu.Lock()
		at, ok := store.attempted["lt_timer"]
		store.mu.Unlock()
		if ok {
			if at.Before(dueAt) || at.Sub(dueAt) > 500*time.Millisecond {
				t.Fatalf("attempted at %v, due at %v", at, dueAt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled test was never dispatched")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// After the dispatch nothing is scheduled, so the dispatcher goes back to sleep.
	before, _ := store.counts()
	time.Sleep(2 * time.Second)
	if due, _ := store.counts(); due != before {
		t.Fatalf("dispatcher kept polling after the last test: %d -> %d DueLockTests calls", before, due)
	}
}

func TestLockTestWait(t *testing.T) {
	store := &lockTestCountingStore{attempted: map[string]time.Time{}}
	h := &Hub{store: store, logger: log.New(io.Discard, "", 0)}
	ctx := context.Background()
	check := func(name string, afterDispatch bool, ok func(time.Duration) bool, wantDue bool) {
		t.Helper()
		if got, due := h.lockTestWait(ctx, afterDispatch); !ok(got) || due != wantDue {
			t.Fatalf("%s: wait %v, due %v", name, got, due)
		}
	}
	is := func(want time.Duration) func(time.Duration) bool {
		return func(d time.Duration) bool { return d == want }
	}
	check("nothing scheduled", false, is(lockTestRecheck), false)
	store.tests = []db.LockTest{{TestID: "far", DueAt: time.Now().Add(time.Hour), Status: "scheduled"}}
	check("far test is clamped to the recheck", false, is(lockTestRecheck), false)
	store.tests = append(store.tests, db.LockTest{TestID: "soon", DueAt: time.Now().Add(10 * time.Second), Status: "scheduled"})
	check("soon test", false, func(d time.Duration) bool { return d > 9*time.Second && d <= 10*time.Second }, true)
	// An overdue test left after a dispatch (lost claim, batch limit) is retried like the old ticker, not in a busy loop.
	store.tests = append(store.tests, db.LockTest{TestID: "overdue", DueAt: time.Now().Add(-time.Second), Status: "scheduled"})
	check("overdue test before a dispatch", false, is(0), true)
	check("overdue test after a dispatch", true, is(lockTestRetry), true)
}
