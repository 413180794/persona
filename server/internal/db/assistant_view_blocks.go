package db

import (
	"context"
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

func (d *DB) RecordAssistantViewErrors(ctx context.Context, conversation, machine, item string, errors []string, at time.Time) error {
	if len(errors) == 0 {
		_, err := d.pool.Exec(ctx, `UPDATE assistant_view_errors SET reported=true
			WHERE conversation=$1 AND machine_id=$2 AND item_id=$3 AND NOT reported`, conversation, machine, item)
		return err
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
	_, err = d.pool.Exec(ctx, `INSERT INTO assistant_view_errors (conversation,machine_id,item_id,errors,created_at)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(conversation,machine_id,item_id) DO UPDATE
		SET errors=excluded.errors,reported=false WHERE assistant_view_errors.errors<>excluded.errors`, conversation, machine, item, raw, at)
	return err
}

func (d *DB) TakeAssistantViewErrors(ctx context.Context, conversation, machine string) ([]string, error) {
	rows, err := d.pool.Query(ctx, `UPDATE assistant_view_errors SET reported=true
		WHERE NOT reported AND (conversation,machine_id,item_id) IN (SELECT conversation,machine_id,item_id FROM assistant_view_errors
		WHERE conversation=$1 AND machine_id=$2 AND NOT reported ORDER BY created_at LIMIT 20) RETURNING errors`, conversation, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var current []string
		if err := json.Unmarshal(raw, &current); err != nil {
			return nil, err
		}
		out = append(out, current...)
	}
	return out, rows.Err()
}
