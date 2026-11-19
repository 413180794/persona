package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/admission"
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
	// Operation is assigned by the local admission gate, never by an extension.
	Operation admission.LedgerOperation `json:"-"`
}

// How many rows one read of the ledger gives: by default, and at most.
const (
	SentAsUserDefault = 50
	SentAsUserMost    = 200
)

var (
	ErrSentRecordConflict = errors.New("sent ledger message reference is ambiguous or has different contents")
	ErrSentRecordPruned   = errors.New("sent ledger entry has been pruned")
)

const insertSentAsUser = `INSERT INTO sent_as_user (at, connector, action, target, text, summary, origin, let_through, message_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

// RecordSentAsUser adds a row (At: now when zero) and returns its id. An empty JSON field is an empty object.
// Legacy extension records still append unless the exact RPC has claimed their reference.
func (d *DB) RecordSentAsUser(ctx context.Context, s SentAsUser) (int64, error) {
	if s.Operation.ID != "" || (s.Action == "sent.record" && s.MessageRef != "") {
		return d.recordExtensionSent(ctx, s, sentRecordLegacy)
	}
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
// unambiguous and the normalized contents match. The guard survives ledger pruning.
// Lookup-only calls use a read-only snapshot and never establish a guard.
func (d *DB) RecordSentAsUserOnce(ctx context.Context, s SentAsUser, lookupOnly bool) (int64, error) {
	if s.MessageRef == "" {
		return 0, errors.New("sent record requires a message reference")
	}
	if lookupOnly {
		return d.recordExtensionSent(ctx, s, sentRecordLookup)
	}
	return d.recordExtensionSent(ctx, s, sentRecordExact)
}

type sentRecordMode uint8

const (
	sentRecordLegacy sentRecordMode = iota
	sentRecordExact
	sentRecordLookup
)

func (d *DB) recordExtensionSent(ctx context.Context, s SentAsUser, mode sentRecordMode) (int64, error) {
	opts := pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	if mode == sentRecordLookup {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := d.pool.BeginTx(ctx, opts)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if mode != sentRecordLookup && s.Operation.ID != "" {
		if err := verifyLedgerAuthority(ctx, tx, s.Operation.Authority); err != nil {
			return 0, err
		}
	}
	if mode != sentRecordLookup && s.MessageRef != "" {
		// Both RPCs take the same lock before reading so writers see the preceding commit.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('sent.record_once:' || $1), hashtext($2))`,
			s.Connector, s.MessageRef); err != nil {
			return 0, err
		}
	}
	key := sha256.Sum256([]byte(s.Connector + "\x00" + s.Action + "\x00" + s.MessageRef))
	var guarded bool
	if s.MessageRef != "" {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sent_record_guards WHERE key = $1)`, key[:]).Scan(&guarded); err != nil {
			return 0, err
		}
	}
	if mode == sentRecordLegacy && !guarded {
		id, err := insertSent(ctx, tx, s)
		if err != nil {
			return 0, err
		}
		return commitSentRecord(ctx, tx, s.Operation.ID, id)
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
	default:
		if guarded {
			return 0, ErrSentRecordPruned
		}
		if mode == sentRecordLookup {
			return 0, tx.Commit(ctx)
		}
		id, err = insertSent(ctx, tx, s)
		if err != nil {
			return 0, err
		}
	}
	if mode == sentRecordExact && !guarded {
		if _, err := tx.Exec(ctx, `INSERT INTO sent_record_guards (key) VALUES ($1)`, key[:]); err != nil {
			return 0, err
		}
	}
	if mode == sentRecordLookup {
		return id, tx.Commit(ctx)
	}
	return commitSentRecord(ctx, tx, s.Operation.ID, id)
}

// A receipt proves that this invocation reached a committed terminal success,
// even if it only reused an existing ledger row or that row is pruned later.
func commitSentRecord(ctx context.Context, tx pgx.Tx, operation string, id int64) (int64, error) {
	if operation != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO sent_record_operations (operation_id) VALUES ($1)`, operation); err != nil {
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
