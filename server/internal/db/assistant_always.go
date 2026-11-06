package db

import (
	"context"
	"time"
)

// AlwaysGrant identifies a stored grant without exposing the command or path used as its key.
// Older records keep only the hash, so their exact scope cannot be reconstructed.
type AlwaysGrant struct {
	KeyHash   string    `json:"key_hash"`
	ToolName  string    `json:"tool_name"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) AlwaysGrants(ctx context.Context, session string) ([]AlwaysGrant, error) {
	rows, err := d.pool.Query(ctx, `SELECT key_hash, tool_name, created_at FROM assistant_always
		WHERE session_id = $1 ORDER BY created_at, key_hash`, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []AlwaysGrant{}
	for rows.Next() {
		var grant AlwaysGrant
		if err := rows.Scan(&grant.KeyHash, &grant.ToolName, &grant.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

// RevokeAlways is idempotent and never removes a grant from another conversation.
func (d *DB) RevokeAlways(ctx context.Context, session, hash string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM assistant_always WHERE session_id = $1 AND key_hash = $2`, session, hash)
	return err
}
