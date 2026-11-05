package hub

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// memStore keeps interaction outcomes like the interactions table; other Store methods are not used here.
type memStore struct {
	Store
	mu     sync.Mutex
	status map[string]string
	res    map[string]*proto.Resolution
}

func newMemStore() *memStore {
	return &memStore{status: map[string]string{}, res: map[string]*proto.Resolution{}}
}

func (m *memStore) InsertInteraction(_ context.Context, ix proto.Interaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.status[ix.ID]; !ok {
		m.status[ix.ID] = "pending"
	}
	return nil
}

func (m *memStore) FinishInteraction(_ context.Context, id, status string, res *proto.Resolution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status[id] == "pending" {
		m.status[id], m.res[id] = status, res
	}
	return nil
}

func (m *memStore) InteractionOutcome(_ context.Context, id string) (string, *proto.Resolution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status[id], m.res[id], nil
}

func (m *memStore) Setting(context.Context, string, any) (bool, error) { return false, nil }

// PutSetting keeps nothing, like Setting finds nothing: loading the schedule writes its one-time task list.
func (m *memStore) PutSetting(context.Context, string, any) error { return nil }

func waitPending(t *testing.T, h *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.pendingInteractions()) != n {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want %d", len(h.pendingInteractions()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func rawOf(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
