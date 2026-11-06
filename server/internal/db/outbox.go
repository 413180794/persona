package db

import (
	"context"
	"time"
)

// ---- the manager's outbox (manager_outbox) ----

// OutboxItem is one thing a manager hands the hub once it is back: a push or an apps' live update, in the order made.
type OutboxItem struct {
	Seq       int64
	ID        string
	Kind      string // push, broadcast
	Body      []byte // as the live call would have sent it
	CreatedAt time.Time
}

// OutboxAdd keeps it for the hub (made at it.CreatedAt; an id kept already is not kept twice). Live updates made
// before staleBefore go now (they would not be handed over), and of it.Kind only the newest keep are kept: the oldest
// go first, and one kind never crowds out the other.
func (d *DB) OutboxAdd(ctx context.Context, it OutboxItem, keep int, staleBefore time.Time) error {
	if _, err := d.pool.Exec(ctx, `INSERT INTO manager_outbox (id, kind, body, created_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`, it.ID, it.Kind, it.Body, it.CreatedAt); err != nil {
		return err
	}
	if _, err := d.pool.Exec(ctx, `DELETE FROM manager_outbox WHERE kind = 'broadcast' AND created_at < $1`, staleBefore); err != nil {
		return err
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM manager_outbox WHERE kind = $1 AND seq <= (SELECT seq FROM manager_outbox WHERE kind = $1
		ORDER BY seq DESC OFFSET $2 LIMIT 1)`, it.Kind, keep)
	return err
}

// OutboxItems are the oldest limit items kept, in order.
func (d *DB) OutboxItems(ctx context.Context, limit int) ([]OutboxItem, error) {
	rows, err := d.pool.Query(ctx, `SELECT seq, id, kind, body, created_at FROM manager_outbox ORDER BY seq LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.Seq, &it.ID, &it.Kind, &it.Body, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// OutboxCount is how many are kept.
func (d *DB) OutboxCount(ctx context.Context) (int, error) {
	var n int
	err := d.pool.QueryRow(ctx, `SELECT count(*) FROM manager_outbox`).Scan(&n)
	return n, err
}

// OutboxDone removes the items the hub took (or that are no use any more).
func (d *DB) OutboxDone(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM manager_outbox WHERE id = ANY($1)`, ids)
	return err
}

// ---- what managers handed the hub (handed_over) ----

// HandOverFirst records manager's item id and reports whether it is new (not handed over within keep); older records
// go.
func (d *DB) HandOverFirst(ctx context.Context, manager, id string, keep time.Duration) (bool, error) {
	if _, err := d.pool.Exec(ctx, `DELETE FROM handed_over WHERE at < now() - $1::interval`, keep.String()); err != nil {
		return false, err
	}
	tag, err := d.pool.Exec(ctx, `INSERT INTO handed_over (manager, id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, manager, id)
	return tag.RowsAffected() == 1, err
}

// HandOverForget drops the record of manager's item id (it was not delivered: it may come again).
func (d *DB) HandOverForget(ctx context.Context, manager, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM handed_over WHERE manager = $1 AND id = $2`, manager, id)
	return err
}
