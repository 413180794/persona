package db

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/extension"
)

// ---- plugins (docs/extensions.md) ----

// Plugins are the registry's rows.
func (d *DB) Extensions(ctx context.Context) ([]extension.Record, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, source, origin, version, previous_version, manifest, granted, granted_digest, enabled,
		config, remote, disabled_contributions, installed_at, updated_at FROM plugins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []extension.Record
	for rows.Next() {
		var r extension.Record
		var granted, remote, disabled []byte
		if err := rows.Scan(&r.ID, &r.Source, &r.Origin, &r.Version, &r.PreviousVersion, &r.Manifest, &granted, &r.GrantedDigest, &r.Enabled,
			&r.Config, &remote, &disabled, &r.InstalledAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(granted, &r.Granted); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(disabled, &r.DisabledContributions); err != nil {
			return nil, err
		}
		if len(remote) > 0 && string(remote) != "null" {
			r.Remote = &extension.Remote{}
			if err := json.Unmarshal(remote, r.Remote); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutPlugin writes a plugin's row.
func (d *DB) PutExtension(ctx context.Context, r extension.Record) error {
	granted, err := json.Marshal(r.Granted)
	if err != nil {
		return err
	}
	var remote []byte
	if r.Remote != nil {
		if remote, err = json.Marshal(r.Remote); err != nil {
			return err
		}
	}
	config := r.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	disabled := r.DisabledContributions
	if disabled == nil {
		disabled = []string{}
	}
	off, err := json.Marshal(disabled)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO plugins (id, source, origin, version, previous_version, manifest, granted, granted_digest, enabled,
		config, remote, installed_at, updated_at, disabled_contributions) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET source = $2, origin = $3, version = $4, previous_version = $5, manifest = $6, granted = $7,
		granted_digest = $8, enabled = $9, config = $10, remote = $11, updated_at = $13, disabled_contributions = $14`,
		r.ID, r.Source, r.Origin, r.Version, r.PreviousVersion, []byte(r.Manifest), granted, r.GrantedDigest, r.Enabled,
		[]byte(config), remote, r.InstalledAt, r.UpdatedAt, off)
	return err
}

// DeletePlugin deletes a plugin's row and (cascading) its secrets.
func (d *DB) DeleteExtension(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM plugins WHERE id = $1`, id)
	return err
}

// PluginSecrets are a plugin's sealed secrets by name.
func (d *DB) ExtensionSecrets(ctx context.Context, id string) (map[string]string, error) {
	rows, err := d.pool.Query(ctx, `SELECT name, sealed FROM plugin_secrets WHERE plugin = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, sealed string
		if err := rows.Scan(&name, &sealed); err != nil {
			return nil, err
		}
		out[name] = sealed
	}
	return out, rows.Err()
}

func (d *DB) PutExtensionSecret(ctx context.Context, id, name, sealed string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO plugin_secrets (plugin, name, sealed, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (plugin, name) DO UPDATE SET sealed = $3, updated_at = now()`, id, name, sealed)
	return err
}

func (d *DB) DeleteExtensionSecret(ctx context.Context, id, name string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM plugin_secrets WHERE plugin = $1 AND name = $2`, id, name)
	return err
}

// AddPluginAudit records a change to a plugin.
func (d *DB) AddExtensionAudit(ctx context.Context, id, action, actor string, detail json.RawMessage) error {
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO plugin_audit (plugin, action, actor, detail) VALUES ($1, $2, $3, $4)`, id, action, actor, []byte(detail))
	return err
}

var _ extension.Store = (*DB)(nil)

// ---- plugin markets (docs/extensions.md「扩展市场」) ----

// PluginMarkets are the registered markets, oldest first.
func (d *DB) ExtensionMarkets(ctx context.Context) ([]extension.MarketRecord, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, spec, name, token_sealed, index_json, warnings, error, fetched_at, added_at,
		signing_key, signature FROM plugin_markets ORDER BY added_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []extension.MarketRecord
	for rows.Next() {
		var r extension.MarketRecord
		var index, warnings, signature []byte
		if err := rows.Scan(&r.ID, &r.Spec, &r.Name, &r.TokenSealed, &index, &warnings, &r.Error, &r.FetchedAt, &r.AddedAt,
			&r.SigningKey, &signature); err != nil {
			return nil, err
		}
		if len(signature) > 0 && string(signature) != "null" {
			if err := json.Unmarshal(signature, &r.Signature); err != nil {
				return nil, err
			}
		}
		if len(index) > 0 && string(index) != "null" {
			r.Index = index
		}
		if err := json.Unmarshal(warnings, &r.Warnings); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutPluginMarket writes a market's row.
func (d *DB) PutExtensionMarket(ctx context.Context, r extension.MarketRecord) error {
	warnings, err := json.Marshal(append([]string{}, r.Warnings...))
	if err != nil {
		return err
	}
	var index []byte
	if len(r.Index) > 0 {
		index = r.Index
	}
	signature, err := json.Marshal(r.Signature)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO plugin_markets (id, spec, name, token_sealed, index_json, warnings, error, fetched_at, added_at,
		signing_key, signature)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET spec = $2, name = $3, token_sealed = $4, index_json = $5, warnings = $6, error = $7, fetched_at = $8,
			signing_key = $10, signature = $11`,
		r.ID, r.Spec, r.Name, r.TokenSealed, index, warnings, r.Error, r.FetchedAt, r.AddedAt, r.SigningKey, signature)
	return err
}

// DeletePluginMarket forgets a market (the plugins installed from it stay).
func (d *DB) DeleteExtensionMarket(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM plugin_markets WHERE id = $1`, id)
	return err
}
