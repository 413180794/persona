package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Peer is a hub this one is paired with (migration 0151, hub/peer_store.go).
type Peer struct {
	HubID     string
	Name      string
	FP        string   // the peer's TLS key pin
	Endpoints []string // its https candidates, and http:// ones with mesh.plaintext (hub/peer_plain.go)
	// Token is the credential the peer issued to this hub, sealed by the hub (never stored in the clear).
	Token string
	// CredentialID is the device token this hub issued to the peer.
	CredentialID string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (d *DB) Peers(ctx context.Context) ([]Peer, error) {
	rows, err := d.pool.Query(ctx, `SELECT hub_id, name, fp, endpoints, token, credential_id, created_at, updated_at
		FROM peers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Peer, error) {
		var p Peer
		var eps []byte
		err := r.Scan(&p.HubID, &p.Name, &p.FP, &eps, &p.Token, &p.CredentialID, &p.CreatedAt, &p.UpdatedAt)
		if err == nil {
			err = json.Unmarshal(eps, &p.Endpoints)
		}
		return p, err
	})
}

// SavePeer adds or replaces a peer (pairing again replaces the credentials both ways).
func (d *DB) SavePeer(ctx context.Context, p Peer) error {
	eps, _ := json.Marshal(nonNil(p.Endpoints))
	_, err := d.pool.Exec(ctx, `INSERT INTO peers (hub_id, name, fp, endpoints, token, credential_id) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (hub_id) DO UPDATE SET name = $2, fp = $3, endpoints = $4, token = $5, credential_id = $6, updated_at = now()`,
		p.HubID, p.Name, p.FP, eps, p.Token, p.CredentialID)
	return err
}

// UpdatePeerSeen keeps what a live peer says about itself: its name and addresses.
func (d *DB) UpdatePeerSeen(ctx context.Context, hubID, name string, endpoints []string) error {
	eps, _ := json.Marshal(nonNil(endpoints))
	_, err := d.pool.Exec(ctx, `UPDATE peers SET name = $2, endpoints = $3, updated_at = now() WHERE hub_id = $1`, hubID, name, eps)
	return err
}

// DeletePeer forgets a peer; it reports whether there was one.
func (d *DB) DeletePeer(ctx context.Context, hubID string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM peers WHERE hub_id = $1`, hubID)
	return err == nil && tag.RowsAffected() > 0, err
}

func (d *DB) CreatePeerCode(ctx context.Context, hash string, expiresAt time.Time) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO peer_codes (code_hash, expires_at) VALUES ($1, $2)`, hash, expiresAt)
	return err
}

// PeerCodeValid: the code exists, was not used and has not expired (a pairing request may go ahead).
func (d *DB) PeerCodeValid(ctx context.Context, hash string) (bool, error) {
	var ok bool
	err := d.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM peer_codes WHERE code_hash = $1 AND used_at IS NULL
		AND expires_at > clock_timestamp())`, hash).Scan(&ok)
	return ok, err
}

// RedeemPeerCode pairs with the hub that brought a code, in one transaction: the code is used up, the credential
// issued to that hub created (its hash), any earlier one revoked, and the peer saved. False, and nothing changed,
// when the code is unknown, used or expired.
func (d *DB) RedeemPeerCode(ctx context.Context, codeHash string, p Peer, cred DeviceToken, credHash string) (bool, error) {
	var redeemed bool
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE peer_codes SET used_at = clock_timestamp(), hub_id = $2
			WHERE code_hash = $1 AND used_at IS NULL AND expires_at > clock_timestamp()`, codeHash, p.HubID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE device_tokens SET revoked_at = now() WHERE kind = 'peer' AND machine_id = $1 AND revoked_at IS NULL`,
			p.HubID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash, machine_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			cred.ID, cred.Name, cred.Kind, cred.Role, credHash, cred.MachineID); err != nil {
			return err
		}
		eps, _ := json.Marshal(nonNil(p.Endpoints))
		if _, err := tx.Exec(ctx, `INSERT INTO peers (hub_id, name, fp, endpoints, token, credential_id) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (hub_id) DO UPDATE SET name = $2, fp = $3, endpoints = $4, token = $5, credential_id = $6, updated_at = now()`,
			p.HubID, p.Name, p.FP, eps, p.Token, p.CredentialID); err != nil {
			return err
		}
		redeemed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return redeemed, nil
}

// PrunePeerCodes drops codes long expired or used.
func (d *DB) PrunePeerCodes(ctx context.Context, before time.Time) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM peer_codes WHERE expires_at < $1`, before)
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
