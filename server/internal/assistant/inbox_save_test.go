package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// inboxBatchMemStore is an inboxMemStore that also writes many items at once, as the database does (PutInboxItems).
type inboxBatchMemStore struct {
	*inboxMemStore
	batches   [][]string
	batchFail error // PutInboxItems fails, storing none
}

func (s *inboxBatchMemStore) PutInboxItems(ctx context.Context, rows []db.InboxRow) error {
	s.imu.Lock()
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	s.batches = append(s.batches, ids)
	fail := s.batchFail
	s.imu.Unlock()
	if fail != nil {
		return fail
	}
	for _, r := range rows {
		_ = s.PutInboxItem(ctx, r.ID, r.Data, r.UpdatedAt, r.DoneAt)
	}
	return nil
}

func newInboxBatchService(t *testing.T) (*Service, *inboxBatchMemStore) {
	t.Helper()
	st := &inboxBatchMemStore{inboxMemStore: newInboxMemStore()}
	s := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return st }, Logf: testLogf(t)})
	if err := s.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, st
}

func inboxSaveItems(n int) []*InboxItem {
	items := make([]*InboxItem, n)
	for i := range items {
		items[i] = &InboxItem{ID: fmt.Sprintf("example:%d", i), Source: "example", Kind: "review_requested", Title: "示例", UpdatedAt: time.Now()}
	}
	return items
}

func registerInboxSaveItems(s *Service, items []*InboxItem) {
	s.inbox.mu.Lock()
	defer s.inbox.mu.Unlock()
	for _, it := range items {
		s.inbox.items[it.ID] = it
	}
}

// A round's changed items reach a store that writes many at once in one write, not one write each.
func TestSaveInboxWritesOnceForMany(t *testing.T) {
	s, st := newInboxBatchService(t)
	items := inboxSaveItems(300)
	registerInboxSaveItems(s, items)
	s.saveInbox(context.Background(), items)
	if len(st.batches) != 1 || len(st.batches[0]) != 300 {
		t.Fatalf("batches = %d, want one of 300", len(st.batches))
	}
	for i := range 300 {
		if id := fmt.Sprintf("example:%d", i); !st.saved(id) || st.ids[id] != 1 {
			t.Fatalf("%s written %d times", id, st.ids[id])
		}
	}
}

// When writing them at once fails, each is written on its own: one that fails does not keep the others out.
func TestSaveInboxFallsBackOneByOne(t *testing.T) {
	s, st := newInboxBatchService(t)
	st.batchFail = errors.New("batch refused")
	items := inboxSaveItems(3)
	registerInboxSaveItems(s, items)
	s.saveInbox(context.Background(), items)
	if len(st.batches) != 1 {
		t.Fatalf("batches = %d", len(st.batches))
	}
	for i := range 3 {
		if id := fmt.Sprintf("example:%d", i); !st.saved(id) || st.ids[id] != 1 {
			t.Fatalf("%s written %d times after the batch failed", id, st.ids[id])
		}
	}
}

// BenchmarkSaveInbox is saveInbox writing a round's n changed items to Postgres (PERSONA_TEST_DATABASE_URL).
func BenchmarkSaveInbox(b *testing.B) {
	url := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if url == "" {
		b.Skip("PERSONA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, url, db.ManagerSet)
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	if err := database.TruncateForTest(ctx); err != nil {
		b.Fatal(err)
	}
	s := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return database }, Logf: func(string, ...any) {}})
	if err := s.inboxReady(ctx); err != nil {
		b.Fatal(err)
	}
	for _, n := range []int{10, 300} {
		items := inboxSaveItems(n)
		registerInboxSaveItems(s, items)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				s.saveInbox(ctx, items)
			}
		})
	}
}
