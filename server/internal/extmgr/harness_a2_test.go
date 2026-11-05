package extmgr

import (
	"context"
	"sync"
)

// a2SentStore keeps sent_messages like the table does (the once-only guard of the inbox's replies).
type a2SentStore struct {
	*memStore
	mu   sync.Mutex
	rows map[string]string
}

func a2NewSentStore() *a2SentStore {
	return &a2SentStore{memStore: newMemStore(), rows: map[string]string{}}
}

func (s *a2SentStore) ClaimSend(_ context.Context, id, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.rows[id]; ok {
		return st, nil
	}
	s.rows[id] = sendSending
	return "", nil
}

func (s *a2SentStore) SettleSend(_ context.Context, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == "" {
		delete(s.rows, id)
	} else {
		s.rows[id] = state
	}
	return nil
}
