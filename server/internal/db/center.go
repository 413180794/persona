package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// MeshGroup is the center the hub self takes its group to have, and the term that made it so (migration 0191,
// hub/mesh_group.go). ok is false before anything was saved for self.
func (d *DB) MeshGroup(ctx context.Context, self string) (center string, term int64, ok bool, err error) {
	err = d.pool.QueryRow(ctx, `SELECT center, term FROM mesh_group WHERE hub_id = $1`, self).Scan(&center, &term)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false, nil
	}
	return center, term, err == nil, err
}

func (d *DB) SaveMeshGroup(ctx context.Context, self, center string, term int64) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO mesh_group (hub_id, center, term) VALUES ($1, $2, $3)
		ON CONFLICT (hub_id) DO UPDATE SET center = $2, term = $3, updated_at = now()`, self, center, term)
	return err
}

// ExportedToken is a device credential as one hub hands it to its group's center: the hash, never the credential.
type ExportedToken struct {
	DeviceToken
	Hash string `json:"hash"`
}

// AppTokens is every live credential of an app (roles full and answer: not a persona-link's or another hub's), with
// its hash, for the center to take over.
func (d *DB) AppTokens(ctx context.Context) ([]ExportedToken, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, name, kind, role, created_at, last_seen, token_hash FROM device_tokens
		WHERE revoked_at IS NULL AND role IN ('full', 'answer') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExportedToken, error) {
		var t ExportedToken
		err := r.Scan(&t.ID, &t.Name, &t.Kind, &t.Role, &t.CreatedAt, &t.LastSeen, &t.Hash)
		return t, err
	})
}

// ImportDeviceTokens keeps the credentials another hub of the group issued (their ids, names, roles and hashes), so
// the devices paired there work here; ones already known (by id or hash) are left as they are. It returns how many
// were new.
func (d *DB) ImportDeviceTokens(ctx context.Context, list []ExportedToken) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		for _, t := range list {
			created := t.CreatedAt
			if created.IsZero() {
				created = time.Now()
			}
			tag, err := tx.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash, created_at, last_seen)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`, t.ID, t.Name, t.Kind, t.Role, t.Hash, created, t.LastSeen)
			if err != nil {
				return err
			}
			n += int(tag.RowsAffected())
		}
		return nil
	})
	return n, err
}

// RevokedAppTokens is every revoked credential of an app, for the other hubs of the group: each keeps its own copy.
func (d *DB) RevokedAppTokens(ctx context.Context) ([]ExportedToken, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, name, kind, role, token_hash FROM device_tokens
		WHERE revoked_at IS NOT NULL AND role IN ('full', 'answer') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExportedToken, error) {
		var t ExportedToken
		err := r.Scan(&t.ID, &t.Name, &t.Kind, &t.Role, &t.Hash)
		return t, err
	})
}

// tombstoneKind marks a credential this hub only knows as revoked elsewhere. It keeps a copy handed over later dead
// (ImportDeviceTokens leaves known ones as they are) and is no device of this hub's, so DeviceTokens leaves it out.
const tombstoneKind = "tombstone"

// ApplyRevokedTokens revokes this hub's copies of credentials another hub revoked and returns the ids of those that were
// live here. One it does not know is kept as a tombstone. Only an app's credential is touched, whatever the list says
// its role is. One statement, not an UPDATE and then an INSERT: a copy being imported right now is waited for and
// revoked, where those two would both miss it. An entry whose id is taken under another hash is skipped: it can only be a
// hub's mistake, and failing on it would block every revocation after it. The times are this hub's.
func (d *DB) ApplyRevokedTokens(ctx context.Context, list []ExportedToken) ([]string, error) {
	var ids []string
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		for _, t := range list {
			var id string
			var tombstone bool
			err := tx.QueryRow(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash, revoked_at)
				SELECT $1::text, $2::text, $3::text, $4::text, $5::text, now()
				WHERE NOT EXISTS (SELECT 1 FROM device_tokens WHERE id = $1 AND token_hash <> $5)
				ON CONFLICT (token_hash) DO UPDATE SET revoked_at = now()
				WHERE device_tokens.revoked_at IS NULL AND device_tokens.role IN ('full', 'answer')
				RETURNING id, kind = $3`, t.ID, t.Name, tombstoneKind, t.Role, t.Hash).Scan(&id, &tombstone)
			if errors.Is(err, pgx.ErrNoRows) { // already revoked, not an app's, or that id is taken
				continue
			}
			if err != nil {
				return err
			}
			if tombstone {
				continue
			}
			if err := dropDeviceRows(ctx, tx, id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

// ConnectorCredential is what pairing with a computer's connector left (kind service, devices/service.go).
type ConnectorCredential struct {
	MachineID string
	FP        string // the connector's TLS key pin
	Token     string // the credential the hub gave it, sealed by the hub
	Version   string // the connector's build when it was paired
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (d *DB) ConnectorCredential(ctx context.Context, machine string) (*ConnectorCredential, error) {
	var c ConnectorCredential
	err := d.pool.QueryRow(ctx, `SELECT machine_id, fp, token, version, created_at, updated_at FROM connector_credentials
		WHERE machine_id = $1`, machine).Scan(&c.MachineID, &c.FP, &c.Token, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveConnectorCredential adds or replaces a machine's (pairing again replaces the pin and the credential).
func (d *DB) SaveConnectorCredential(ctx context.Context, c ConnectorCredential) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO connector_credentials (machine_id, fp, token, version) VALUES ($1, $2, $3, $4)
		ON CONFLICT (machine_id) DO UPDATE SET fp = $2, token = $3, version = $4, updated_at = now()`,
		c.MachineID, c.FP, c.Token, c.Version)
	return err
}

func (d *DB) DeleteConnectorCredential(ctx context.Context, machine string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM connector_credentials WHERE machine_id = $1`, machine)
	return err == nil && tag.RowsAffected() > 0, err
}
