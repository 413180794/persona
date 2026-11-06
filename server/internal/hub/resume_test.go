package hub

import (
	"context"
	"sync"

	"github.com/nwverse/persona/server/internal/proto"
)

// memStore keeps interaction outcomes like the interactions table; other Store methods are not used here.
type memStore struct {
	sharedStore
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

func (m *memStore) TakeSetting(context.Context, string, any) (bool, error) { return false, nil }
