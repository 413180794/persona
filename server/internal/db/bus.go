package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BusIdentity is a service that may connect to the hub's bus (migrations 0311 and 0419, hub/bustokens.go). For a
// manager it is its entry in the roster (hub/managers.go): where its last handshake said it runs, and when it was
// last connected.
type BusIdentity struct {
	ID        string
	Role      string // manager (the only role so far)
	Name      string
	CreatedAt time.Time // when its current token was made
	Host      string
	OS        string
	Arch      string
	Deploy    string // child (the hub started it), service, docker, external
	Version   string
	LastSeen  *time.Time // when it was last connected (its handshake, or the moment it dropped); nil: never
}

const busIdentityColumns = `id, role, name, created_at, host, os, arch, deploy, version, last_seen`

func scanBusIdentity(r pgx.Row) (BusIdentity, error) {
	var b BusIdentity
	err := r.Scan(&b.ID, &b.Role, &b.Name, &b.CreatedAt, &b.Host, &b.OS, &b.Arch, &b.Deploy, &b.Version, &b.LastSeen)
	return b, err
}

// SaveBusIdentity adds an identity, or gives an existing one a new role, name and token (the old token stops working).
// Where it runs and when it was last seen stay.
func (d *DB) SaveBusIdentity(ctx context.Context, b BusIdentity, tokenHash string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO bus_identities (id, role, name, token_hash) VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET role = $2, name = $3, token_hash = $4, created_at = now()`, b.ID, b.Role, b.Name, tokenHash)
	return err
}

// BusIdentityByHash is the identity whose token hashes to hash; nil when there is none.
func (d *DB) BusIdentityByHash(ctx context.Context, hash string) (*BusIdentity, error) {
	return d.oneBusIdentity(ctx, `SELECT `+busIdentityColumns+` FROM bus_identities WHERE token_hash = $1`, hash)
}

// BusIdentityByID is identity id; nil when there is none.
func (d *DB) BusIdentityByID(ctx context.Context, id string) (*BusIdentity, error) {
	return d.oneBusIdentity(ctx, `SELECT `+busIdentityColumns+` FROM bus_identities WHERE id = $1`, id)
}

func (d *DB) oneBusIdentity(ctx context.Context, query string, arg string) (*BusIdentity, error) {
	b, err := scanBusIdentity(d.pool.QueryRow(ctx, query, arg))
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
	rows, err := d.pool.Query(ctx, `SELECT `+busIdentityColumns+` FROM bus_identities ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BusIdentity, error) { return scanBusIdentity(r) })
}

// SeeBusIdentity records that identity id is connected now. With where set (its handshake) it also keeps where it
// runs; without, only the time (it dropped).
func (d *DB) SeeBusIdentity(ctx context.Context, id string, where *BusIdentity) error {
	if where == nil {
		_, err := d.pool.Exec(ctx, `UPDATE bus_identities SET last_seen = now() WHERE id = $1`, id)
		return err
	}
	_, err := d.pool.Exec(ctx, `UPDATE bus_identities SET host = $2, os = $3, arch = $4, deploy = $5, version = $6,
		last_seen = now() WHERE id = $1`, id, where.Host, where.OS, where.Arch, where.Deploy, where.Version)
	return err
}

// RenameBusIdentity gives identity id a new name; false when there is none.
func (d *DB) RenameBusIdentity(ctx context.Context, id, name string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `UPDATE bus_identities SET name = $2 WHERE id = $1`, id, name)
	return err == nil && tag.RowsAffected() > 0, err
}

// DeleteBusIdentity removes identity id, and its install codes; it reports whether there was one.
func (d *DB) DeleteBusIdentity(ctx context.Context, id string) (bool, error) {
	if _, err := d.pool.Exec(ctx, `DELETE FROM manager_install_codes WHERE manager_id = $1`, id); err != nil {
		return false, err
	}
	tag, err := d.pool.Exec(ctx, `DELETE FROM bus_identities WHERE id = $1`, id)
	return err == nil && tag.RowsAffected() > 0, err
}

// ManagerInstallCode is a one-time code a new manager trades for its bus token (hub/managers.go).
type ManagerInstallCode struct {
	ManagerID string
	Name      string
	ExpiresAt time.Time
}

// AddManagerInstallCode keeps the sha256 of a new install code for manager c.ManagerID. Codes that expired or were
// used more than a day ago go at the same time.
func (d *DB) AddManagerInstallCode(ctx context.Context, hash string, c ManagerInstallCode) error {
	if _, err := d.pool.Exec(ctx, `DELETE FROM manager_install_codes
		WHERE expires_at < now() - interval '1 day' OR used_at < now() - interval '1 day'`); err != nil {
		return err
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO manager_install_codes (code_hash, manager_id, name, expires_at)
		VALUES ($1, $2, $3, $4)`, hash, c.ManagerID, c.Name, c.ExpiresAt)
	return err
}

// UseManagerInstallCode marks the code that hashes to hash used, once: nil when there is none, it expired or it was
// used already.
func (d *DB) UseManagerInstallCode(ctx context.Context, hash string) (*ManagerInstallCode, error) {
	var c ManagerInstallCode
	err := d.pool.QueryRow(ctx, `UPDATE manager_install_codes SET used_at = now()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING manager_id, name, expires_at`, hash).Scan(&c.ManagerID, &c.Name, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ManagerInstallCodeByHash is the code that hashes to hash while it is still good (not used, not expired), without
// using it; nil when there is none.
func (d *DB) ManagerInstallCodeByHash(ctx context.Context, hash string) (*ManagerInstallCode, error) {
	var c ManagerInstallCode
	err := d.pool.QueryRow(ctx, `SELECT manager_id, name, expires_at FROM manager_install_codes
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > now()`, hash).Scan(&c.ManagerID, &c.Name, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// PendingManagerInstalls are the managers that have a code still good and no identity yet (being installed), by id.
func (d *DB) PendingManagerInstalls(ctx context.Context) ([]ManagerInstallCode, error) {
	rows, err := d.pool.Query(ctx, `SELECT DISTINCT ON (c.manager_id) c.manager_id, c.name, c.expires_at
		FROM manager_install_codes c LEFT JOIN bus_identities b ON b.id = c.manager_id
		WHERE b.id IS NULL AND c.used_at IS NULL AND c.expires_at > now()
		ORDER BY c.manager_id, c.expires_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ManagerInstallCode, error) {
		var c ManagerInstallCode
		err := r.Scan(&c.ManagerID, &c.Name, &c.ExpiresAt)
		return c, err
	})
}

// DropManagerInstallCodes removes manager id's codes that were not used (a pending install called off).
func (d *DB) DropManagerInstallCodes(ctx context.Context, id string) (int64, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM manager_install_codes WHERE manager_id = $1 AND used_at IS NULL`, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
