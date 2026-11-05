package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AssistantViewActionRow struct {
	Conversation string
	Key          string
	Action       string
	Page         string
	Values       json.RawMessage
	ClientMsgID  string
	MessageID    string
	MachineID    string
	Text         string
	Status       string
	Error        string
	At           time.Time
	UpdatedAt    time.Time
	Revision     int64
	LeaseUntil   time.Time
	// Version is the card version the user confirmed (0: committed before it was recorded).
	Version int64
	// Kind is "action" (a declared reply) or "html" (an HTML bridge reply: Action is the component key).
	Kind string
	// Reply is an HTML bridge reply's confirmed text; Text is the full message sent to the assistant.
	Reply string
}

const viewActionColumns = `conversation, key, action, page, values, client_msg_id, message_id, machine_id, text, status, error, at, updated_at, revision, version, kind, reply`

func scanViewAction(row pgx.Row) (*AssistantViewActionRow, error) {
	var a AssistantViewActionRow
	err := row.Scan(&a.Conversation, &a.Key, &a.Action, &a.Page, &a.Values, &a.ClientMsgID, &a.MessageID, &a.MachineID, &a.Text, &a.Status, &a.Error, &a.At, &a.UpdatedAt, &a.Revision, &a.Version, &a.Kind, &a.Reply)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// BeginAssistantViewAction reserves one reply per card, storing its text before any delivery attempt.
// expected binds the server-validated action to the view version still stored at commit time, and is recorded as the
// version the user confirmed. Any existing selection (an action or an HTML reply) is returned instead.
func (d *DB) BeginAssistantViewAction(ctx context.Context, a AssistantViewActionRow, expected int64) (*AssistantViewActionRow, bool, error) {
	if a.Conversation == "" || a.Key == "" || a.Action == "" || a.ClientMsgID == "" || a.MessageID == "" || a.MachineID == "" || a.Text == "" || a.At.IsZero() {
		return nil, false, errors.New("assistant action identity, message and time are required")
	}
	if len(a.Values) == 0 {
		a.Values = json.RawMessage(`{}`)
	}
	if a.Kind == "" {
		a.Kind = "action"
	}
	if expected < 1 || a.Kind != "action" && a.Kind != "html" || (a.Kind == "html") != (a.Reply != "") {
		return nil, false, errors.New("assistant action version, kind or reply is invalid")
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var version int64
	err = tx.QueryRow(ctx, `SELECT version FROM assistant_views WHERE conversation=$1 AND id=$2 AND machine_id=$3 FOR UPDATE`, a.Conversation, a.Key, a.MachineID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT 1 FROM assistant_view_blocks WHERE conversation=$1 AND key=$2 AND machine_id=$3 FOR UPDATE`, a.Conversation, a.Key, a.MachineID).Scan(&version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrAssistantViewNotFound
	}
	if err != nil {
		return nil, false, err
	}
	old, err := scanViewAction(tx.QueryRow(ctx, `SELECT `+viewActionColumns+` FROM assistant_view_actions WHERE conversation=$1 AND key=$2`, a.Conversation, a.Key))
	if err != nil {
		return nil, false, err
	}
	if old != nil {
		return old, false, tx.Commit(ctx)
	}
	if version != expected {
		return nil, false, ErrAssistantViewVersion
	}
	_, err = tx.Exec(ctx, `INSERT INTO assistant_view_actions (conversation,key,action,page,values,client_msg_id,message_id,machine_id,text,status,at,updated_at,version,kind,reply)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',$10,$10,$11,$12,$13)`, a.Conversation, a.Key, a.Action, a.Page, []byte(a.Values), a.ClientMsgID, a.MessageID, a.MachineID, a.Text, a.At, expected, a.Kind, a.Reply)
	if err != nil {
		return nil, false, err
	}
	a.Status = "pending"
	a.Revision = 1
	a.Version = expected
	a.UpdatedAt = a.At
	return &a, true, tx.Commit(ctx)
}

func (d *DB) AssistantViewAction(ctx context.Context, conversation, key string) (*AssistantViewActionRow, error) {
	return scanViewAction(d.pool.QueryRow(ctx, `SELECT `+viewActionColumns+` FROM assistant_view_actions WHERE conversation=$1 AND key=$2`, conversation, key))
}

func (d *DB) AssistantViewActions(ctx context.Context, conversation string) ([]AssistantViewActionRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT `+viewActionColumns+` FROM assistant_view_actions WHERE conversation=$1 ORDER BY at,key`, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssistantViewActionRow{}
	for rows.Next() {
		a, err := scanViewAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ClaimAssistantViewDelivery leases the outbox row. Another process cannot start the same delivery concurrently.
func (d *DB) ClaimAssistantViewDelivery(ctx context.Context, conversation, key string, until time.Time) (*AssistantViewActionRow, error) {
	until = until.UTC().Truncate(time.Microsecond)
	a, err := scanViewAction(d.pool.QueryRow(ctx, `UPDATE assistant_view_actions SET claimed_until=$3,updated_at=now()
		WHERE conversation=$1 AND key=$2 AND status='pending' AND (claimed_until IS NULL OR claimed_until<now()) RETURNING `+viewActionColumns, conversation, key, until))
	if a != nil {
		a.LeaseUntil = until
	}
	return a, err
}

func (d *DB) SettleAssistantViewDelivery(ctx context.Context, conversation, key string, lease time.Time, status, reason string) error {
	if lease.IsZero() || status != "sent" && status != "failed" && status != "pending" {
		return errors.New("invalid assistant action delivery status or lease")
	}
	tag, err := d.pool.Exec(ctx, `UPDATE assistant_view_actions SET status=$3,error=$4,revision=revision+1,claimed_until=NULL,updated_at=now()
		WHERE conversation=$1 AND key=$2 AND status='pending' AND claimed_until=$5`, conversation, key, status, reason, lease)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("assistant action delivery lease changed")
	}
	return err
}

func (d *DB) PendingAssistantViewActions(ctx context.Context, limit int) ([]AssistantViewActionRow, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := d.pool.Query(ctx, `SELECT `+viewActionColumns+` FROM assistant_view_actions WHERE status='pending'
		AND (claimed_until IS NULL OR claimed_until<now()) ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssistantViewActionRow{}
	for rows.Next() {
		a, err := scanViewAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
