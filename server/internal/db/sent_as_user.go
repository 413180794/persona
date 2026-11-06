package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// The ledger of what was written as the user (migrations/0420_sent_as_user.sql, docs/connectors.md "Sent as the user").

// SentAsUser is one write that went out through a connector's gate.
type SentAsUser struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Connector string    `json:"connector"`
	Action    string    `json:"action"`
	// Target is where it went, by connector; Origin the conversation that asked; LetThrough how the gate let it
	// through (all JSON objects).
	Target     json.RawMessage `json:"target"`
	Text       string          `json:"text"`
	Summary    string          `json:"summary"`
	Origin     json.RawMessage `json:"origin"`
	LetThrough json.RawMessage `json:"let_through"`
	MessageRef string          `json:"message_ref,omitempty"`
	RecalledAt *time.Time      `json:"recalled_at,omitempty"`
}

// How many rows one read of the ledger gives: by default, and at most.
const (
	SentAsUserDefault = 50
	SentAsUserMost    = 200
)

// RecordSentAsUser adds a row (At: now when zero) and returns its id. An empty JSON field is an empty object.
func (d *DB) RecordSentAsUser(ctx context.Context, s SentAsUser) (int64, error) {
	if s.At.IsZero() {
		s.At = time.Now()
	}
	var id int64
	err := d.pool.QueryRow(ctx, `INSERT INTO sent_as_user (at, connector, action, target, text, summary, origin, let_through, message_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		s.At, s.Connector, s.Action, objectOrEmpty(s.Target), s.Text, s.Summary, objectOrEmpty(s.Origin), objectOrEmpty(s.LetThrough),
		s.MessageRef).Scan(&id)
	return id, err
}

func objectOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// SentAsUsers are the ledger's rows newest first (by id): those before the id before (0: from the newest), at most
// limit (SentAsUserDefault when below 1, at most SentAsUserMost), of connector when it is set. next is the id to ask
// for the page after this one (before = next), 0 when this is the last.
func (d *DB) SentAsUsers(ctx context.Context, before int64, limit int, connector string) (rows []SentAsUser, next int64, err error) {
	if limit < 1 {
		limit = SentAsUserDefault
	}
	limit = min(limit, SentAsUserMost)
	res, err := d.pool.Query(ctx, `SELECT id, at, connector, action, target, text, summary, origin, let_through, message_ref, recalled_at
		FROM sent_as_user WHERE ($1::bigint = 0 OR id < $1::bigint) AND ($2 = '' OR connector = $2) ORDER BY id DESC LIMIT $3`,
		before, connector, limit+1)
	if err != nil {
		return nil, 0, err
	}
	rows, err = pgx.CollectRows(res, func(r pgx.CollectableRow) (SentAsUser, error) {
		var s SentAsUser
		var target, origin, through []byte
		err := r.Scan(&s.ID, &s.At, &s.Connector, &s.Action, &target, &s.Text, &s.Summary, &origin, &through, &s.MessageRef, &s.RecalledAt)
		s.Target, s.Origin, s.LetThrough = target, origin, through
		return s, err
	})
	if err != nil {
		return nil, 0, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[limit-1].ID
	}
	return rows, next, nil
}

// PruneSentAsUser deletes every row written before before and says how many there were.
func (d *DB) PruneSentAsUser(ctx context.Context, before time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM sent_as_user WHERE at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
