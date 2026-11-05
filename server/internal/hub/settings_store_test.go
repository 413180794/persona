package hub

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/nwverse/persona/server/internal/db"
)

// settingsStore keeps settings in memory like the settings table.
type settingsStore struct {
	Store
	mu   sync.Mutex
	vals map[string][]byte
	puts int
}

func (s *settingsStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.vals[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(b, out)
}

func (s *settingsStore) PutSetting(_ context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vals[key] = b
	s.puts++
	return nil
}

func (s *settingsStore) ListTodos(context.Context) ([]db.TodoRow, error) { return nil, nil }
func (s *settingsStore) ListAllTodoSpaces(context.Context) ([]db.TodoSpaceRow, error) {
	return nil, nil
}
func (s *settingsStore) ListAllTodoAgents(context.Context) ([]db.TodoAgentRow, error) {
	return nil, nil
}
