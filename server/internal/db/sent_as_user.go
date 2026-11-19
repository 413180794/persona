package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The ledger of what was written as the user (migrations/0420_sent_as_user.sql, docs/extensions/connectors.md "Sent as the user").

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

var ErrSentRecordConflict = errors.New("sent ledger message reference is ambiguous or has different contents")

const insertSentAsUser = `INSERT INTO sent_as_user (at, connector, action, target, text, summary, origin, let_through, message_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

// RecordSentAsUser adds a row (At: now when zero) and returns its id. An empty JSON field is an empty object.
func (d *DB) RecordSentAsUser(ctx context.Context, s SentAsUser) (int64, error) {
	return insertSent(ctx, d.pool, s)
}

func insertSent(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, s SentAsUser) (int64, error) {
	if s.At.IsZero() {
		s.At = time.Now()
	}
	var id int64
	err := q.QueryRow(ctx, insertSentAsUser,
		s.At, s.Connector, s.Action, objectOrEmpty(s.Target), s.Text, s.Summary, objectOrEmpty(s.Origin), objectOrEmpty(s.LetThrough),
		s.MessageRef).Scan(&id)
	return id, err
}

// RecordSentAsUserOnce reuses a retained ledger row only when the reference is
// unambiguous and the normalized contents match. Lookup-only calls never insert.
// The transaction lock serializes concurrent calls using this contract.
func (d *DB) RecordSentAsUserOnce(ctx context.Context, s SentAsUser, lookupOnly bool) (int64, error) {
	if s.MessageRef == "" {
		return 0, errors.New("sent record requires a message reference")
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('sent.record_once:' || $1), hashtext($2))`,
		s.Connector, s.MessageRef); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id, target = $4::jsonb AND text = $5 AND summary = $6 AND origin = $7::jsonb AND let_through = $8::jsonb
		FROM sent_as_user WHERE connector = $1 AND action = $2 AND message_ref = $3 LIMIT 2`,
		s.Connector, s.Action, s.MessageRef, objectOrEmpty(s.Target), s.Text, s.Summary, objectOrEmpty(s.Origin), objectOrEmpty(s.LetThrough))
	if err != nil {
		return 0, err
	}
	type match struct {
		id    int64
		exact bool
	}
	matches, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (v match, err error) {
		err = r.Scan(&v.id, &v.exact)
		return v, err
	})
	if err != nil {
		return 0, err
	}
	var id int64
	switch {
	case len(matches) == 1 && matches[0].exact:
		id = matches[0].id
	case len(matches) != 0:
		return 0, ErrSentRecordConflict
	case !lookupOnly:
		id, err = insertSent(ctx, tx, s)
		if err != nil {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
}

func objectOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// sentAsUserColumns are a row's columns in the order scanSentAsUser reads them.
const sentAsUserColumns = `id, at, connector, action, target, text, summary, origin, let_through, message_ref, recalled_at`

func scanSentAsUser(r pgx.Row) (SentAsUser, error) {
	var s SentAsUser
	var target, origin, through []byte
	err := r.Scan(&s.ID, &s.At, &s.Connector, &s.Action, &target, &s.Text, &s.Summary, &origin, &through, &s.MessageRef, &s.RecalledAt)
	s.Target, s.Origin, s.LetThrough = target, origin, through
	return s, err
}

// SentAsUsers are the ledger's rows newest first (by id): those before the id before (0: from the newest), at most
// limit (SentAsUserDefault when below 1, at most SentAsUserMost), of connector when it is set. next is the id to ask
// for the page after this one (before = next), 0 when this is the last.
func (d *DB) SentAsUsers(ctx context.Context, before int64, limit int, connector string) (rows []SentAsUser, next int64, err error) {
	if limit < 1 {
		limit = SentAsUserDefault
	}
	limit = min(limit, SentAsUserMost)
	res, err := d.pool.Query(ctx, `SELECT `+sentAsUserColumns+`
		FROM sent_as_user WHERE ($1::bigint = 0 OR id < $1::bigint) AND ($2 = '' OR connector = $2) ORDER BY id DESC LIMIT $3`,
		before, connector, limit+1)
	if err != nil {
		return nil, 0, err
	}
	rows, err = pgx.CollectRows(res, func(r pgx.CollectableRow) (SentAsUser, error) { return scanSentAsUser(r) })
	if err != nil {
		return nil, 0, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[limit-1].ID
	}
	return rows, next, nil
}

const sentAsUsersForArchiveSQL = `SELECT ` + sentAsUserColumns + ` FROM sent_as_user
	WHERE connector = $1 AND at >= $2 AND at < $3 ORDER BY at, id`

// SentAsUsersForArchive reads one connector's ledger in [since, until), oldest first.
// Archive matching needs every candidate, including recalled messages still present in the archive;
// the settings page's pagination limit does not apply here.
func (d *DB) SentAsUsersForArchive(ctx context.Context, connector string, since, until time.Time) ([]SentAsUser, error) {
	rows, err := d.pool.Query(ctx, sentAsUsersForArchiveSQL, connector, since, until)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SentAsUser, error) { return scanSentAsUser(r) })
}

// PruneSentAsUser deletes every row written before before and says how many there were.
func (d *DB) PruneSentAsUser(ctx context.Context, before time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM sent_as_user WHERE at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SentAsUserByID is the row with the id id; ok is false when there is none.
func (d *DB) SentAsUserByID(ctx context.Context, id int64) (row SentAsUser, ok bool, err error) {
	row, err = scanSentAsUser(d.pool.QueryRow(ctx, `SELECT `+sentAsUserColumns+` FROM sent_as_user WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return SentAsUser{}, false, nil
	}
	return row, err == nil, err
}

// MarkSentAsUserRecalled sets recalled_at of the row with the id id to at and says whether it did: not when the row is
// gone, or was marked before (the first time stays, whoever asks again).
func (d *DB) MarkSentAsUserRecalled(ctx context.Context, id int64, at time.Time) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE sent_as_user SET recalled_at = $2 WHERE id = $1 AND recalled_at IS NULL`, id, at)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
