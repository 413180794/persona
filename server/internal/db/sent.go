package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ClaimSend records a message about to be sent. A client_msg_id seen before returns that send's state instead
// ("sending", "delivered", "unknown"); "" means this one is new and now recorded as sending.
func (d *DB) ClaimSend(ctx context.Context, id, machine, session string) (string, error) {
	// A restored outbox can retry much later. Expiring an unacknowledged operation makes an old retry execute again.
	tag, err := d.pool.Exec(ctx, `INSERT INTO sent_messages (client_msg_id, machine_id, session_id, state)
		VALUES ($1, $2, $3, 'sending') ON CONFLICT DO NOTHING`, id, machine, session)
	if err != nil || tag.RowsAffected() == 1 {
		return "", err
	}
	var state string
	err = d.pool.QueryRow(ctx, `SELECT state FROM sent_messages WHERE client_msg_id = $1`, id).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) { // pruned in between
		return d.ClaimSend(ctx, id, machine, session)
	}
	return state, err
}

// SettleSend records how a send ended; "" forgets it (nothing was typed: a retry may send it).
func (d *DB) SettleSend(ctx context.Context, id, state string) error {
	if state == "" {
		_, err := d.pool.Exec(ctx, `DELETE FROM sent_messages WHERE client_msg_id = $1`, id)
		return err
	}
	_, err := d.pool.Exec(ctx, `UPDATE sent_messages SET state = $2 WHERE client_msg_id = $1`, id, state)
	return err
}
