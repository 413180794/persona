package assistant

import (
	"context"
	"testing"
	"time"
)

// A failed read of the "Please verify" asks leaves them unloaded: the next look reads them again, and nothing is written over
// the stored cards meanwhile (an empty map saved after the failure would forget them for good).
func TestAcceptanceFailedReadIsRetriedNotSavedOver(t *testing.T) {
	s, _, st := modelService(t)
	ctx := context.Background()
	stored := map[string]*acceptAsk{"a": {MessageID: "m1", Messages: []string{"m1"}, AskedAt: time.Now().UTC().Add(-time.Hour)}}
	if err := st.memStore.PutSetting(ctx, acceptanceKey, stored); err != nil {
		t.Fatal(err)
	}
	storedAsks := func() map[string]*acceptAsk {
		t.Helper()
		var got map[string]*acceptAsk
		if _, err := st.memStore.Setting(ctx, acceptanceKey, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	st.failing(true)
	s.accept.mu.Lock()
	if s.loadAcceptLocked(ctx) {
		t.Error("a failed read reported the asks as loaded")
	}
	if s.accept.loaded {
		t.Error("a failed read marked the asks loaded")
	}
	s.accept.mu.Unlock()
	if id, _ := s.acceptFor("m1"); id != "" {
		t.Errorf("acceptFor while unreadable = %q, want none", id)
	}
	if s.accepted(ctx, "a", "") {
		t.Error("accepted while unreadable")
	}
	if list, n := s.snapshotUnaccepted(ctx, time.UTC); len(list) != 0 || n != 0 {
		t.Errorf("snapshotUnaccepted while unreadable = %v, %d", list, n)
	}

	// The store answers again, but memory still holds nothing: a save now must not write it over the cards.
	st.failing(false)
	s.accept.mu.Lock()
	s.saveAcceptLocked(ctx)
	s.accept.mu.Unlock()
	if got := storedAsks(); got["a"] == nil || got["a"].MessageID != "m1" {
		t.Fatalf("stored asks after a save while unloaded = %v, want a: m1", got)
	}

	s.accept.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		t.Error("the read after recovery failed")
	}
	n := len(s.accept.asks)
	s.accept.mu.Unlock()
	if n != 1 {
		t.Fatalf("asks in memory after recovery = %d, want 1", n)
	}
	if id, _ := s.acceptFor("m1"); id != "a" {
		t.Errorf("acceptFor after recovery = %q, want a", id)
	}
	if a := askOf(s, "a"); a == nil || a.MessageID != "m1" {
		t.Errorf("ask a after recovery = %+v", a)
	}
}
