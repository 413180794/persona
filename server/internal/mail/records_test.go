package mail

import (
	"context"
	"sync"

	"github.com/chyroc/jarvis/server/internal/db"
)

// memRecords is an in-memory Records for tests (production uses Postgres via internal/db).
type memRecords struct {
	mu   sync.Mutex
	rows []db.MailAccountRow
}

func newMemRecords() *memRecords { return &memRecords{} }

func (m *memRecords) MailAccounts(context.Context) ([]db.MailAccountRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]db.MailAccountRow(nil), m.rows...), nil
}

func (m *memRecords) ReplaceMailAccounts(_ context.Context, rows []db.MailAccountRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append([]db.MailAccountRow(nil), rows...)
	return nil
}

// dump concatenates everything persisted, to check no secret is stored in clear text.
func (m *memRecords) dump() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s string
	for _, r := range m.rows {
		s += r.ID + r.Email + string(r.Data) + r.PasswordEnc + r.OAuthEnc
	}
	return s
}
