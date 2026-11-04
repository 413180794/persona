package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BusIdentity is a service that may connect to the hub's bus (migration 0311, hub/bustokens.go).
type BusIdentity struct {
	ID        string
	Role      string // manager (the only role so far)
	Name      string
	CreatedAt time.Time // when its current token was made
}

// SaveBusIdentity adds an identity, or gives an existing one a new role, name and token (the old token stops working).
func (d *DB) SaveBusIdentity(ctx context.Context, b BusIdentity, tokenHash string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO bus_identities (id, role, name, token_hash) VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET role = $2, name = $3, token_hash = $4, created_at = now()`, b.ID, b.Role, b.Name, tokenHash)
	return err
}

// BusIdentityByHash is the identity whose token hashes to hash; nil when there is none.
func (d *DB) BusIdentityByHash(ctx context.Context, hash string) (*BusIdentity, error) {
	var b BusIdentity
	err := d.pool.QueryRow(ctx, `SELECT id, role, name, created_at FROM bus_identities WHERE token_hash = $1`, hash).
		Scan(&b.ID, &b.Role, &b.Name, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BusIdentities lists every identity, by id.
func (d *DB) BusIdentities(ctx context.Context) ([]BusIdentity, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, role, name, created_at FROM bus_identities ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BusIdentity, error) {
		var b BusIdentity
		err := r.Scan(&b.ID, &b.Role, &b.Name, &b.CreatedAt)
		return b, err
	})
}

// DeleteBusIdentity removes identity id; it reports whether there was one.
func (d *DB) DeleteBusIdentity(ctx context.Context, id string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM bus_identities WHERE id = $1`, id)
	return err == nil && tag.RowsAffected() > 0, err
}
