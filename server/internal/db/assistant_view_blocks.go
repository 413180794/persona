package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/sdk/go/wire"
)

type AssistantViewBlockRow struct {
	Conversation string
	Key          string
	MachineID    string
	ItemID       string
	Index        int
	View         json.RawMessage
	CreatedAt    time.Time
}

// RegisterAssistantViewBlock is called only after a trusted node item was checked by the assistant.
// Replays do not change the original definition or clear the user's selection.
func (d *DB) RegisterAssistantViewBlock(ctx context.Context, b AssistantViewBlockRow) (bool, error) {
	if b.Conversation == "" || b.MachineID == "" || b.ItemID == "" || b.Key != wire.AssistantBlockKey(b.ItemID, b.Index) || b.Index < 0 || b.Index >= 3 || b.CreatedAt.IsZero() {
		return false, errors.New("assistant block needs trusted provenance and a stable key")
	}
	if err := checkAssistantViewJSON(b.View); err != nil {
		return false, err
	}
	tag, err := d.pool.Exec(ctx, `INSERT INTO assistant_view_blocks (conversation,key,machine_id,item_id,block_index,view,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(conversation,key) DO NOTHING`, b.Conversation, b.Key, b.MachineID, b.ItemID, b.Index, []byte(b.View), b.CreatedAt)
	return tag.RowsAffected() == 1, err
}

// AssistantCard reads either a live card or an immutable transcript card from this conversation.
func (d *DB) AssistantCard(ctx context.Context, conversation, key string) (*AssistantViewRow, error) {
	v, err := d.AssistantView(ctx, conversation, key)
	if !errors.Is(err, ErrAssistantViewNotFound) {
		return v, err
	}
	var row AssistantViewRow
	err = d.pool.QueryRow(ctx, `SELECT key,conversation,machine_id,view,created_at FROM assistant_view_blocks
		WHERE conversation=$1 AND key=$2`, conversation, key).Scan(&row.ID, &row.Conversation, &row.MachineID, &row.View, &row.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssistantViewNotFound
	}
	if err != nil {
		return nil, err
	}
	row.Version = 1
	row.UpdatedAt = row.CreatedAt
	return &row, nil
}

func (d *DB) AssistantViewBlockKeys(ctx context.Context, conversation string) ([]string, error) {
	rows, err := d.pool.Query(ctx, `SELECT key FROM assistant_view_blocks WHERE conversation=$1 ORDER BY created_at,key`, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// RecordAssistantViewErrors keeps the problems of one assistant reply's views until the conversation is told
// (PeekAssistantViewErrors, AckAssistantViewErrors). The same problems replayed are not reported again; changed ones
// are. errors empty means the reply drew valid views only: that clears its own problems and, with a time, those of
// replies written before at, now and when they are recorded later (the conversation's pass mark). A zero at clears only
// its own.
func (d *DB) RecordAssistantViewErrors(ctx context.Context, conversation, machine, item string, errors []string, at time.Time) error {
	if conversation == "" || machine == "" || item == "" {
		return nil
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if len(errors) == 0 {
		if at.IsZero() { // a replay without a time: only its own, the mark stays
			if _, err := tx.Exec(ctx, `UPDATE assistant_view_errors SET reported=true
				WHERE conversation=$1 AND machine_id=$2 AND item_id=$3 AND NOT reported`, conversation, machine, item); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		// The mark first, then the error rows: every writer locks in that order (lockViewErrorMark).
		if _, err := tx.Exec(ctx, `INSERT INTO assistant_view_error_marks (conversation,machine_id,passed_at) VALUES($1,$2,$3)
			ON CONFLICT(conversation,machine_id) DO UPDATE SET passed_at=greatest(assistant_view_error_marks.passed_at,excluded.passed_at)`,
			conversation, machine, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE assistant_view_errors SET reported=true
			WHERE conversation=$1 AND machine_id=$2 AND NOT reported AND (item_id=$3 OR created_at<$4)`, conversation, machine, item, at); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	bounded := make([]string, min(len(errors), 20))
	for i := range bounded {
		text := []rune(errors[i])
		bounded[i] = string(text[:min(len(text), 2000)])
	}
	raw, err := json.Marshal(bounded)
	if err != nil {
		return err
	}
	passed, err := lockViewErrorMark(ctx, tx, conversation, machine)
	if err != nil {
		return err
	}
	redone := !at.IsZero() && !at.After(passed) // written no later than a reply that passed (or than itself, passing)
	if _, err := tx.Exec(ctx, `INSERT INTO assistant_view_errors (conversation,machine_id,item_id,errors,reported,created_at)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(conversation,machine_id,item_id) DO UPDATE
		SET errors=excluded.errors,reported=excluded.reported WHERE assistant_view_errors.errors<>excluded.errors`,
		conversation, machine, item, raw, redone, at); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// lockViewErrorMark locks the conversation's pass mark (made if missing) and reads it. Every write of the conversation's
// error rows takes it first, so concurrent writers queue on one row instead of locking error rows in different orders
// (a pass and a replay of the same reply deadlocked when the pass locked the error row first).
func lockViewErrorMark(ctx context.Context, tx pgx.Tx, conversation, machine string) (time.Time, error) {
	var passed time.Time
	err := tx.QueryRow(ctx, `INSERT INTO assistant_view_error_marks (conversation,machine_id,passed_at) VALUES($1,$2,'epoch')
		ON CONFLICT(conversation,machine_id) DO UPDATE SET passed_at=assistant_view_error_marks.passed_at RETURNING passed_at`,
		conversation, machine).Scan(&passed)
	return passed, err
}

// AssistantViewErrorRow is one reply's unreported view problems; Digest names them as read (AckAssistantViewErrors).
type AssistantViewErrorRow struct {
	ItemID    string
	Errors    []string
	Digest    string
	CreatedAt time.Time
}

// assistantViewErrorsKept is how long unreported problems wait for the conversation's next message.
const assistantViewErrorsKept = 7 * 24 * time.Hour

// PeekAssistantViewErrors reads the oldest unreported problems of a conversation on a machine, at most limit replies.
// It changes nothing: a read for a message that is never delivered must not count as told.
func (d *DB) PeekAssistantViewErrors(ctx context.Context, conversation, machine string, limit int) ([]AssistantViewErrorRow, error) {
	if conversation == "" || machine == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx, `SELECT item_id,errors::text,created_at FROM assistant_view_errors
		WHERE conversation=$1 AND machine_id=$2 AND NOT reported AND created_at>$3
		ORDER BY created_at,item_id LIMIT $4`, conversation, machine, time.Now().Add(-assistantViewErrorsKept), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AssistantViewErrorRow
	for rows.Next() {
		var r AssistantViewErrorRow
		var raw string
		if err := rows.Scan(&r.ItemID, &raw, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r.Errors); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(raw))
		r.Digest = hex.EncodeToString(sum[:])
		out = append(out, r)
	}
	return out, rows.Err()
}

// AckAssistantViewErrors marks problems told once the message carrying them was delivered. Only those still as they
// were read (item and digest) are marked: problems recorded since stay for the next message.
func (d *DB) AckAssistantViewErrors(ctx context.Context, conversation, machine string, items, digests []string) (int64, error) {
	if conversation == "" || machine == "" || len(items) == 0 || len(items) != len(digests) {
		return 0, nil
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := lockViewErrorMark(ctx, tx, conversation, machine); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE assistant_view_errors e SET reported=true
		FROM unnest($3::text[],$4::text[]) AS r(item_id,digest)
		WHERE e.conversation=$1 AND e.machine_id=$2 AND NOT e.reported AND e.item_id=r.item_id
		AND encode(sha256(convert_to(e.errors::text,'UTF8')),'hex')=r.digest`, conversation, machine, items, digests)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}
