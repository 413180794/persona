package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type PairToken struct {
	Hash      string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	DeviceID  string
	// MachineID: made for one machine's persona-link (`personad hub pair --machine`); it redeems only that credential.
	MachineID string
}

// endedNow is the expiry a revocation gives a code (a pairing code, a manager install code; $2: the hub's time.Now()),
// never later than the one it had. Redemption checks the expiry by the database's clock (RedeemPairToken), the hub by
// its own (#6543, #6553): the code is over by both, also when Postgres runs in a VM whose clock is ahead of the hub's.
const endedNow = `LEAST(expires_at, now(), $2)`

func (d *DB) CreatePairToken(ctx context.Context, hash string, expiresAt time.Time, machine string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO pair_tokens (token_hash, expires_at, machine_id) VALUES ($1, $2, NULLIF($3, ''))`,
		hash, expiresAt, machine)
	return err
}

func (d *DB) PairTokenByHash(ctx context.Context, hash string) (*PairToken, error) {
	var p PairToken
	err := d.pool.QueryRow(ctx, `SELECT token_hash, created_at, expires_at, used_at, COALESCE(device_id, ''), COALESCE(machine_id, '')
		FROM pair_tokens WHERE token_hash = $1`, hash).Scan(&p.Hash, &p.CreatedAt, &p.ExpiresAt, &p.UsedAt, &p.DeviceID, &p.MachineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// RedeemPairToken consumes the pairing credential and issues a device credential in the same transaction.
// A concurrent redemption, an expired credential or a failed insert cannot leave an extra device token.
func (d *DB) RedeemPairToken(ctx context.Context, pairHash string, device DeviceToken, tokenHash string) (bool, error) {
	var redeemed bool
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE pair_tokens SET used_at = clock_timestamp(), device_id = $2
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > clock_timestamp()`, pairHash, device.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash, machine_id)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`, device.ID, device.Name, device.Kind, device.Role, tokenHash, device.MachineID)
		redeemed = err == nil
		return err
	})
	return redeemed && err == nil, err
}
