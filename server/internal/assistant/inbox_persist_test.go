package assistant

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// inboxOrderStore delays the first write so an undo can race with it.
type inboxOrderStore struct {
	*inboxMemStore
	mu            sync.Mutex
	calls         atomic.Int32
	firstEntered  chan struct{}
	releaseFirst  chan struct{}
	secondSaved   chan struct{}
	deleteEntered chan struct{}
	deleteOnce    sync.Once
	saved         []byte
	done          *time.Time
}

func (s *inboxOrderStore) PutInboxItem(ctx context.Context, id string, data []byte, updatedAt time.Time, done *time.Time) error {
	n := s.calls.Add(1)
	if n == 1 && s.firstEntered != nil {
		close(s.firstEntered)
		<-s.releaseFirst
	}
	s.mu.Lock()
	s.saved = append([]byte(nil), data...)
	s.done = done
	s.mu.Unlock()
	if err := s.inboxMemStore.PutInboxItem(ctx, id, data, updatedAt, done); err != nil {
		return err
	}
	if n == 2 && s.secondSaved != nil {
		close(s.secondSaved)
	}
	return nil
}

func (s *inboxOrderStore) InboxItems(ctx context.Context, doneSince time.Time) ([][]byte, error) {
	return s.inboxMemStore.InboxItems(ctx, doneSince)
}

func (s *inboxOrderStore) DeleteInboxItems(ctx context.Context, ids []string) error {
	if s.deleteEntered != nil {
		s.deleteOnce.Do(func() { close(s.deleteEntered) })
	}
	return s.inboxMemStore.DeleteInboxItems(ctx, ids)
}

func (s *inboxOrderStore) hasRow(id string) bool {
	s.imu.Lock()
	defer s.imu.Unlock()
	_, ok := s.rows[id]
	return ok
}

func TestInboxPersistenceKeepsLatestState(t *testing.T) {
	s := &inboxOrderStore{
		inboxMemStore: newInboxMemStore(),
		firstEntered:  make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		secondSaved:   make(chan struct{}),
	}
	h := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return s }, Logf: t.Logf})
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.inbox.items["item"] = &InboxItem{ID: "item", Source: "mail", UpdatedAt: time.Now()}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(s.releaseFirst) }) }
	defer release()

	firstDone := make(chan error, 1)
	go func() {
		_, err := h.setInboxDone(context.Background(), "item", true, false)
		firstDone <- err
	}()
	select {
	case <-s.firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first save did not reach the store")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, err := h.setInboxDone(context.Background(), "item", false, false)
		secondDone <- err
	}()
	// A serialized save waits here; without serialization the undo writes first.
	select {
	case <-s.secondSaved:
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an inbox save did not finish")
		}
	}

	s.mu.Lock()
	var stored InboxItem
	err := json.Unmarshal(s.saved, &stored)
	done := s.done
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if stored.DoneAt != nil || stored.DoneBy != "" || done != nil {
		t.Fatalf("the completed save overwrote undo: %+v, done column %v", stored, done)
	}
	restarted := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return s }, Logf: t.Logf})
	if err := restarted.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := restarted.inbox.items["item"]
	if restored == nil || !restored.open() || restored.DoneBy != "" {
		t.Fatalf("undo was lost after restart: %+v", restored)
	}
}

func TestInboxForgetSerializesWithPersistence(t *testing.T) {
	s := &inboxOrderStore{
		inboxMemStore: newInboxMemStore(),
		firstEntered:  make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		secondSaved:   make(chan struct{}),
		deleteEntered: make(chan struct{}),
	}
	h := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return s }, Logf: t.Logf})
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	item := &InboxItem{ID: "item", Source: "mail", UpdatedAt: time.Now()}
	h.inbox.mu.Lock()
	h.inbox.items[item.ID] = item
	h.inbox.mu.Unlock()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(s.releaseFirst) }) }
	defer release()

	firstDone := make(chan error, 1)
	go func() {
		_, err := h.setInboxDone(context.Background(), item.ID, true, false)
		firstDone <- err
	}()
	select {
	case <-s.firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first save did not reach the store")
	}
	forgetDone := make(chan error, 1)
	go func() {
		_, err := h.InboxForget(context.Background(), item.Source)
		forgetDone <- err
	}()
	deletedBeforeSave := false
	select {
	case <-s.deleteEntered:
		deletedBeforeSave = true
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for _, done := range []<-chan error{firstDone, forgetDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an inbox persistence operation did not finish")
		}
	}
	if deletedBeforeSave {
		t.Fatal("InboxForget deleted the row while an older save was still in progress")
	}
	if s.hasRow(item.ID) {
		t.Fatal("the completed snapshot resurrected a forgotten item")
	}
}

func TestSaveInboxSkipsForgottenItem(t *testing.T) {
	s := &inboxOrderStore{inboxMemStore: newInboxMemStore()}
	h := New(Options{Hub: newFakeHub().withMachine(), Store: func() Store { return s }, Logf: t.Logf})
	if err := h.inboxReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	item := &InboxItem{ID: "item", Source: "mail", UpdatedAt: time.Now()}
	h.inbox.mu.Lock()
	h.inbox.items[item.ID] = item
	h.inbox.mu.Unlock()
	if _, err := h.InboxForget(context.Background(), item.Source); err != nil {
		t.Fatal(err)
	}
	// This is the pointer a save that was queued before Forget would carry.
	h.saveInbox(context.Background(), []*InboxItem{item})
	if s.hasRow(item.ID) {
		t.Fatal("a queued save resurrected an item removed by InboxForget")
	}
}
