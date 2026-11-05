// Package db is the hub's Postgres store (docker container jarvis-postgres on the hub machine, see docs/server.md).
package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nwverse/persona/server/internal/proto"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	pool      *pgxpool.Pool
	closeOnce sync.Once
	closeDone chan struct{}
}

// Open connects and applies pending migrations. It retries for a while so the hub can start alongside the container.
func Open(ctx context.Context, url string) (*DB, error) {
	if url == "" {
		return nil, errors.New("database_url is empty (run scripts/deploy/deploy.sh <hub machine id> to create the Postgres container)")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database_url: %w", err)
	}
	cfg.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pctx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("connect postgres: %w", err)
		}
		time.Sleep(time.Second)
	}
	d := &DB{pool: pool}
	if err := d.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() { _ = d.CloseContext(context.Background()) }

// CloseContext stops new acquisitions and bounds waiting for borrowed connections.
// pgxpool has no forced close: outstanding borrowers may finish after ctx expires.
func (d *DB) CloseContext(ctx context.Context) error {
	d.closeOnce.Do(func() {
		d.closeDone = make(chan struct{})
		go func() {
			d.pool.Close()
			close(d.closeDone)
		}()
	})
	select {
	case <-d.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// migrate runs migrations/NNNN_*.sql in order, each in its own transaction, recording them in schema_migrations.
func (d *DB) migrate(ctx context.Context) error {
	return d.migrateFrom(ctx, migrations)
}

// renumbered are migrations a database may have recorded under the number they had before a merge gave them another
// one (two lines of work picked the same number). Such a record would make migrate skip the migration that has the old
// number now, and run the renumbered one again; so it is moved to the new number first, or dropped when the new number
// is recorded too. The renumbered migrations are idempotent; the ones that took over the old numbers never ran there.
var renumbered = []struct{ from, to string }{
	{"0071_pair_tokens.sql", "0111_pair_tokens.sql"},             // 0071 is sent_messages
	{"0081_device_push_relay.sql", "0141_device_push_relay.sql"}, // 0081 is device_sessions
}

// migrateFrom is migrate over the migrations/ directory of fsys (tests start from a database at an older state).
func (d *DB) migrateFrom(ctx context.Context, fsys fs.FS) error {
	if _, err := d.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// Serialize concurrent hubs (and tests) on one advisory lock.
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(424242)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(424242)`) //nolint:errcheck

	for _, r := range renumbered {
		if err := renumberMigration(ctx, conn, r.from, r.to); err != nil {
			return fmt.Errorf("renumber migration %s: %w", r.from, err)
		}
	}
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := fs.ReadFile(fsys, "migrations/"+name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, version, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

// migrationVersion is the number a migration file name starts with.
func migrationVersion(name string) (int, error) {
	version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
	if err != nil {
		return 0, fmt.Errorf("bad migration name %s", name)
	}
	return version, nil
}

// renumberMigration moves a database's record of migration from (applied under its old number) to to's name and
// number; when to is recorded already, the old record only goes away.
func renumberMigration(ctx context.Context, conn *pgxpool.Conn, from, to string) error {
	version, err := migrationVersion(to)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE name = $1
			AND EXISTS (SELECT 1 FROM schema_migrations WHERE version = $2)`, from, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE schema_migrations SET version = $2, name = $3 WHERE name = $1`, from, version, to)
		return err
	})
}

// ---- devices ----

type Device struct {
	DeviceID     string `json:"device_id"`
	CredentialID string `json:"-"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	APNSToken    string `json:"apns_token,omitempty"`
	APNSEnv      string `json:"apns_env,omitempty"`
	Topic        string `json:"topic,omitempty"`
	// Pushing through the relay (docs/push-relay.md): the phone's public key (base64 X9.63), the relay's URL and the
	// grant it gave the phone.
	PushKey    string `json:"push_key,omitempty"`
	RelayURL   string `json:"relay_url,omitempty"`
	RelayGrant string `json:"relay_grant,omitempty"`
	// Credential: the device token (device_tokens.id) that registered it, set by the hub, never from the request;
	// empty for the pairing token. Revoking it forgets the device (hub: Pusher.signedOut).
	Credential string    `json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
	// Prefs is the device's notification settings as JSON (hub decodes it); empty = defaults. Registration
	// (UpsertDevice) never touches it, only SetDevicePrefs does.
	Prefs json.RawMessage `json:"-"`
}

func (d *DB) UpsertDevice(ctx context.Context, v Device) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, push_key, relay_url, relay_grant, credential, updated_at, credential_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), $11)
		ON CONFLICT (device_id) DO UPDATE SET name = $2, kind = $3, apns_token = $4, apns_env = $5, topic = $6,
			push_key = $7, relay_url = $8, relay_grant = $9, credential = $10, updated_at = now(), credential_id = $11`,
		v.DeviceID, v.Name, v.Kind, v.APNSToken, v.APNSEnv, v.Topic, v.PushKey, v.RelayURL, v.RelayGrant, v.Credential, v.CredentialID)
	return err
}

func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, name, kind, apns_token, apns_env, topic, push_key, relay_url, relay_grant, credential, updated_at, prefs, credential_id FROM devices ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var v Device
		var prefs []byte
		err := r.Scan(&v.DeviceID, &v.Name, &v.Kind, &v.APNSToken, &v.APNSEnv, &v.Topic, &v.PushKey, &v.RelayURL, &v.RelayGrant, &v.Credential, &v.UpdatedAt, &prefs, &v.CredentialID)
		if len(prefs) > 0 {
			v.Prefs = prefs
		}
		return v, err
	})
}

// SetDevicePrefs stores a device's notification settings. The device may not have registered for push yet (no
// permission, the simulator): the row is created then and filled in when it does.
func (d *DB) SetDevicePrefs(ctx context.Context, id string, prefs json.RawMessage) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, updated_at, prefs)
		VALUES ($1, '', '', '', '', '', now(), $2)
		ON CONFLICT (device_id) DO UPDATE SET prefs = $2`, id, []byte(prefs))
	return err
}

// DeleteDevice forgets a device (e.g. APNs says the token is no longer valid).
func (d *DB) DeleteDevice(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM devices WHERE device_id = $1`, id)
	return err
}

// LiveActivity is a device's Fleet activity (hub/liveactivity.go).
type LiveActivity struct {
	DeviceID         string
	ActivityID       string
	PushToken        string
	PushToStartToken string
	StartedAt        time.Time
}

func (d *DB) LiveActivities(ctx context.Context) ([]LiveActivity, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, activity_id, push_token, push_to_start_token, started_at FROM live_activities`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveActivity, error) {
		var v LiveActivity
		var started *time.Time
		err := r.Scan(&v.DeviceID, &v.ActivityID, &v.PushToken, &v.PushToStartToken, &started)
		if started != nil {
			v.StartedAt = *started
		}
		return v, err
	})
}

// SaveLiveActivity writes the whole row (the hub keeps the current state in memory and saves it on every change).
func (d *DB) SaveLiveActivity(ctx context.Context, v LiveActivity) error {
	var started *time.Time
	if !v.StartedAt.IsZero() {
		started = &v.StartedAt
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO live_activities (device_id, activity_id, push_token, push_to_start_token, started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (device_id) DO UPDATE SET activity_id = $2, push_token = $3, push_to_start_token = $4, started_at = $5, updated_at = now()`,
		v.DeviceID, v.ActivityID, v.PushToken, v.PushToStartToken, started)
	return err
}

// ---- phone tools ----

// PhoneToolRegistration is one device's most recently announced tool list.
type PhoneToolRegistration struct {
	DeviceID     string
	Platform     string
	CredentialID string
	UpdatedAt    time.Time
	Tools        []proto.Tool
}

// ReplacePhoneTools replaces only this device's tools, including an empty list.
func (d *DB) ReplacePhoneTools(ctx context.Context, registration PhoneToolRegistration) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if registration.CredentialID != "" {
			var id string
			if err := tx.QueryRow(ctx, `SELECT id FROM device_tokens WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, registration.CredentialID).Scan(&id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM phone_tools WHERE device_id = $1`, registration.DeviceID); err != nil {
			return err
		}
		for i, t := range registration.Tools {
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO phone_tools (device_id, platform, credential_id, name, description, input_schema, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (device_id, name) DO UPDATE SET description = $5, input_schema = $6, position = $7, updated_at = now()`,
				registration.DeviceID, registration.Platform, registration.CredentialID, t.Name, t.Description, []byte(schema), i); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) PhoneTools(ctx context.Context) ([]PhoneToolRegistration, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, platform, credential_id, updated_at, name, description, input_schema
		FROM phone_tools ORDER BY device_id, position, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var registrations []PhoneToolRegistration
	for rows.Next() {
		var r PhoneToolRegistration
		var t proto.Tool
		if err := rows.Scan(&r.DeviceID, &r.Platform, &r.CredentialID, &r.UpdatedAt, &t.Name, &t.Description, &t.InputSchema); err != nil {
			return nil, err
		}
		if len(registrations) == 0 || registrations[len(registrations)-1].DeviceID != r.DeviceID {
			registrations = append(registrations, r)
		}
		last := &registrations[len(registrations)-1]
		last.Tools = append(last.Tools, t)
		if r.UpdatedAt.After(last.UpdatedAt) {
			last.UpdatedAt = r.UpdatedAt
		}
	}
	return registrations, rows.Err()
}

// ---- node sessions ----

// NodeSession is what a node remembers about a session it started (see internal/node).
type NodeSession struct {
	Model            string    `json:"model,omitempty"`
	ModelFingerprint string    `json:"model_fingerprint,omitempty"`
	ModelEffort      string    `json:"model_effort,omitempty"`
	Pane             string    `json:"pane,omitempty"`
	Launcher         string    `json:"launcher"`
	Cwd              string    `json:"cwd"`
	StartedAt        time.Time `json:"started_at"`
	Tasks            []string  `json:"tasks,omitempty"` // an assistant conversation's running background tasks
}

func (d *DB) PutNodeSession(ctx context.Context, machine, id string, s NodeSession) error {
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
	tasks := s.Tasks
	if tasks == nil {
		tasks = []string{}
	}
	_, err := d.pool.Exec(ctx, `WITH saved AS (
		INSERT INTO node_sessions (machine_id, session_id, pane, launcher, cwd, started_at, tasks, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (machine_id, session_id) DO UPDATE SET pane = $3, launcher = $4, cwd = $5, started_at = $6, tasks = $7, updated_at = now()
		RETURNING machine_id, session_id
	) INSERT INTO settings (key, value, updated_at)
		SELECT 'node_model.' || machine_id || '.' || session_id,
		jsonb_build_object('model', $8::text, 'model_fingerprint', $9::text, 'model_effort', $10::text), now() FROM saved
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		machine, id, s.Pane, s.Launcher, s.Cwd, s.StartedAt, tasks, s.Model, s.ModelFingerprint, s.ModelEffort)
	return err
}

// NodeSessions returns a machine's sessions from the last 60 days.
func (d *DB) NodeSessions(ctx context.Context, machine string) (map[string]NodeSession, error) {
	rows, err := d.pool.Query(ctx, `SELECT n.session_id, n.pane, n.launcher, n.cwd, n.started_at, n.tasks,
		COALESCE(s.value->>'model',''), COALESCE(s.value->>'model_fingerprint',''), COALESCE(s.value->>'model_effort','')
		FROM node_sessions n LEFT JOIN settings s ON s.key = 'node_model.' || n.machine_id || '.' || n.session_id
		WHERE n.machine_id = $1 AND n.started_at > now() - interval '60 days'`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]NodeSession{}
	for rows.Next() {
		var id string
		var s NodeSession
		if err := rows.Scan(&id, &s.Pane, &s.Launcher, &s.Cwd, &s.StartedAt, &s.Tasks, &s.Model, &s.ModelFingerprint, &s.ModelEffort); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}

// ---- mail accounts ----

// MailAccountRow is one account as the mail package persists it: non-secret JSON plus ciphertext.
type MailAccountRow struct {
	ID          string
	Email       string
	Data        json.RawMessage
	PasswordEnc string
	OAuthEnc    string
}

func (d *DB) MailAccounts(ctx context.Context) ([]MailAccountRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, email, data, password_enc, oauth_enc FROM mail_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MailAccountRow, error) {
		var m MailAccountRow
		var data []byte
		err := r.Scan(&m.ID, &m.Email, &data, &m.PasswordEnc, &m.OAuthEnc)
		m.Data = data
		return m, err
	})
}

// ReplaceMailAccounts makes the table equal to rows (the mail package always saves its whole account set).
func (d *DB) ReplaceMailAccounts(ctx context.Context, rows []MailAccountRow) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
			if _, err := tx.Exec(ctx, `INSERT INTO mail_accounts (id, email, data, password_enc, oauth_enc) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (id) DO UPDATE SET email = $2, data = $3, password_enc = $4, oauth_enc = $5, updated_at = now()`,
				r.ID, r.Email, []byte(r.Data), r.PasswordEnc, r.OAuthEnc); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM mail_accounts WHERE NOT (id = ANY($1))`, ids)
		return err
	})
}

// ---- interactions ----

func (d *DB) InsertInteraction(ctx context.Context, ix proto.Interaction) error {
	payload, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO interactions (id, machine_id, session_id, kind, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (id) DO NOTHING`,
		ix.ID, ix.MachineID, ix.SessionID, ix.Kind, payload, ix.CreatedAt)
	return err
}

// FinishInteraction records how an interaction ended; only the first outcome counts.
func (d *DB) FinishInteraction(ctx context.Context, id, status string, res *proto.Resolution) error {
	var raw []byte
	if res != nil {
		raw, _ = json.Marshal(res)
	}
	_, err := d.pool.Exec(ctx, `UPDATE interactions SET status = $2, resolution = $3, resolved_at = now()
		WHERE id = $1 AND status = 'pending'`, id, status, raw)
	return err
}

// InteractionRecord is a past interaction with its outcome.
type InteractionRecord struct {
	Interaction proto.Interaction `json:"interaction"`
	Status      string            `json:"status"`
	Resolution  *proto.Resolution `json:"resolution,omitempty"`
	ResolvedAt  *time.Time        `json:"resolved_at,omitempty"`
}

// Interactions lists recent interactions, newest first, optionally for one session.
func (d *DB) Interactions(ctx context.Context, machine, session string, limit int) ([]InteractionRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.pool.Query(ctx, `SELECT payload, status, resolution, resolved_at FROM interactions
		WHERE ($1 = '' OR machine_id = $1) AND ($2 = '' OR session_id = $2)
		ORDER BY created_at DESC LIMIT $3`, machine, session, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (InteractionRecord, error) {
		var rec InteractionRecord
		var payload, res []byte
		if err := r.Scan(&payload, &rec.Status, &res, &rec.ResolvedAt); err != nil {
			return rec, err
		}
		if err := json.Unmarshal(payload, &rec.Interaction); err != nil {
			return rec, err
		}
		if len(res) > 0 {
			rec.Resolution = &proto.Resolution{}
			if err := json.Unmarshal(res, rec.Resolution); err != nil {
				return rec, err
			}
		}
		return rec, nil
	})
}

// ExpireStaleInteractions marks interactions left pending by a previous hub process cancelled, and returns the
// resumable ones not yet expired instead: their hooks reconnect and claim them again.
func (d *DB) ExpireStaleInteractions(ctx context.Context) ([]proto.Interaction, error) {
	rows, err := d.pool.Query(ctx, `SELECT payload FROM interactions WHERE status = 'pending'
		AND (payload->>'resumable')::boolean IS TRUE AND (payload->>'expires_at')::timestamptz > now()`)
	if err != nil {
		return nil, err
	}
	keep, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (proto.Interaction, error) {
		var payload []byte
		var ix proto.Interaction
		if err := r.Scan(&payload); err != nil {
			return ix, err
		}
		return ix, json.Unmarshal(payload, &ix)
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(keep))
	for i, ix := range keep {
		ids[i] = ix.ID
	}
	_, err = d.pool.Exec(ctx, `UPDATE interactions SET status = 'cancelled', resolved_at = now()
		WHERE status = 'pending' AND NOT (id = ANY($1))`, ids)
	return keep, err
}

// InteractionOutcome returns an interaction's status and resolution; an empty status when there is no such row.
func (d *DB) InteractionOutcome(ctx context.Context, id string) (string, *proto.Resolution, error) {
	var status string
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT status, resolution FROM interactions WHERE id = $1`, id).Scan(&status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil || len(raw) == 0 {
		return status, nil, err
	}
	var res proto.Resolution
	return status, &res, json.Unmarshal(raw, &res)
}

// ---- 本会话总是允许 on assistant conversations (assistant_always) ----

func alwaysKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// AllowAlways keeps keys as granted for the session (tool: what the card was about, for reading the table).
func (d *DB) AllowAlways(ctx context.Context, session, tool string, keys []string) error {
	hashes := make([]string, len(keys))
	for i, k := range keys {
		hashes[i] = alwaysKeyHash(k)
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO assistant_always (session_id, key_hash, tool_name)
		SELECT $1::text, h, $2::text FROM unnest($3::text[]) AS h ON CONFLICT DO NOTHING`, session, tool, hashes)
	return err
}

// AlwaysAllowed reports whether every one of keys was granted for the session; false for none.
func (d *DB) AlwaysAllowed(ctx context.Context, session string, keys []string) (bool, error) {
	want := map[string]bool{}
	for _, k := range keys {
		want[alwaysKeyHash(k)] = true
	}
	if session == "" || len(want) == 0 {
		return false, nil
	}
	hashes := make([]string, 0, len(want))
	for h := range want {
		hashes = append(hashes, h)
	}
	var n int
	err := d.pool.QueryRow(ctx, `SELECT count(*) FROM assistant_always WHERE session_id = $1 AND key_hash = ANY($2::text[])`,
		session, hashes).Scan(&n)
	return err == nil && n == len(hashes), err
}

// ---- connector secrets ----

// ConnectorSecret is a connector's sealed secret; false when it has none.
func (d *DB) ConnectorSecret(ctx context.Context, connector string) (string, bool, error) {
	var sealed string
	err := d.pool.QueryRow(ctx, `SELECT sealed FROM connector_secrets WHERE connector = $1`, connector).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return sealed, err == nil, err
}

func (d *DB) PutConnectorSecret(ctx context.Context, connector, sealed string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO connector_secrets (connector, sealed, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (connector) DO UPDATE SET sealed = $2, updated_at = now()`, connector, sealed)
	return err
}

func (d *DB) DeleteConnectorSecret(ctx context.Context, connector string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM connector_secrets WHERE connector = $1`, connector)
	return err
}

// ---- settings ----

// Setting decodes the value stored under key into out; it reports false when the key is unset.
func (d *DB) Setting(ctx context.Context, key string, out any) (bool, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, out)
}

func (d *DB) PutSetting(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = d.pool.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`, key, raw)
	return err
}

// TakeSetting is Setting that also removes the key, in one statement: a value meant to be read once (the event log a
// stopping hub hands to the next one, docs/hub-restart.md) is never read twice.
func (d *DB) TakeSetting(ctx context.Context, key string, out any) (bool, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `DELETE FROM settings WHERE key = $1 RETURNING value`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, out)
}

// CompareSetting atomically replaces a versioned setting without losing another writer's update.
// A cleared binding keeps its revision, so restoring defaults does not reset concurrency history.
func (d *DB) CompareSetting(ctx context.Context, key string, revision int64, value any) (bool, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	tag, err := d.pool.Exec(ctx, `INSERT INTO settings (key, value, updated_at)
		SELECT $1, $2::jsonb, now() WHERE $3::bigint = 0
		ON CONFLICT (key) DO UPDATE SET value = $2::jsonb, updated_at = now()
		WHERE COALESCE((settings.value->>'revision')::bigint, 0) = $3`, key, raw, revision)
	if revision != 0 && err == nil && tag.RowsAffected() == 0 {
		tag, err = d.pool.Exec(ctx, `UPDATE settings SET value = $2::jsonb, updated_at = now()
			WHERE key = $1 AND COALESCE((value->>'revision')::bigint, 0) = $3`, key, raw, revision)
	}
	return tag.RowsAffected() == 1, err
}

// ---- session views ----

// MarkSeen records that the user looked at a session now.
func (d *DB) MarkSeen(ctx context.Context, machine, session string, at time.Time) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO session_views (machine_id, session_id, seen_at) VALUES ($1, $2, $3)
		ON CONFLICT (machine_id, session_id) DO UPDATE SET seen_at = GREATEST(session_views.seen_at, $3)`, machine, session, at)
	return err
}

// SeenTimes returns when each session was last looked at (last 30 days).
func (d *DB) SeenTimes(ctx context.Context) (map[[2]string]time.Time, error) {
	rows, err := d.pool.Query(ctx, `SELECT machine_id, session_id, seen_at FROM session_views WHERE seen_at > now() - interval '30 days'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]string]time.Time{}
	for rows.Next() {
		var m, s string
		var t time.Time
		if err := rows.Scan(&m, &s, &t); err != nil {
			return nil, err
		}
		out[[2]string{m, s}] = t
	}
	return out, rows.Err()
}

// ---- knowledge base index ----

// KBEntity is one indexed knowledge-base entity.
type KBEntity struct {
	Kind, ID, Name, Importance string
	Relevance                  int
	Tags                       []string
	OneLine                    string
	UpdatedAt                  *time.Time
}

// PutKBEntity upserts one row of the knowledge-base index.
func (d *DB) PutKBEntity(ctx context.Context, kind, id, name, importance string, relevance int, tags []string, oneLine string, updated time.Time) error {
	if tags == nil {
		tags = []string{}
	}
	var at *time.Time
	if !updated.IsZero() {
		at = &updated
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO kb_entities (kind, id, name, importance, relevance, tags, one_line, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (kind, id) DO UPDATE SET name = $3, importance = $4, relevance = $5, tags = $6, one_line = $7, updated_at = $8`,
		kind, id, name, importance, relevance, tags, oneLine, at)
	return err
}

// DeleteKBEntities removes the index rows of kind with these ids (a source the user took back, kb.ForgetSource).
func (d *DB) DeleteKBEntities(ctx context.Context, kind string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM kb_entities WHERE kind = $1 AND id = ANY($2)`, kind, ids)
	return err
}

// KBEntities lists indexed entities of a kind (all kinds when ""), carrying tag when set, most important first.
func (d *DB) KBEntities(ctx context.Context, kind, tag string, limit int) ([]KBEntity, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.pool.Query(ctx, `SELECT kind, id, name, importance, relevance, tags, one_line, updated_at FROM kb_entities
		WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR $2 = ANY(tags))
		ORDER BY CASE WHEN importance = '' THEN 'P9' ELSE importance END, relevance DESC, updated_at DESC NULLS LAST LIMIT $3`,
		kind, tag, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KBEntity
	for rows.Next() {
		var e KBEntity
		if err := rows.Scan(&e.Kind, &e.ID, &e.Name, &e.Importance, &e.Relevance, &e.Tags, &e.OneLine, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- inbox ----

const putInboxItemSQL = `INSERT INTO inbox_items (id, data, updated_at, done_at) VALUES ($1, $2, $3, $4)
	ON CONFLICT (id) DO UPDATE SET data = $2, updated_at = $3, done_at = $4`

// PutInboxItem stores one inbox item (the manager's inbox, assistant/inbox.go, owns the JSON).
func (d *DB) PutInboxItem(ctx context.Context, id string, data []byte, updatedAt time.Time, doneAt *time.Time) error {
	_, err := d.pool.Exec(ctx, putInboxItemSQL, id, data, updatedAt, doneAt)
	return err
}

// InboxRow is one inbox item for PutInboxItems.
type InboxRow struct {
	ID        string
	Data      []byte
	UpdatedAt time.Time
	DoneAt    *time.Time
}

// PutInboxItems stores inbox items in one round trip and one transaction: when one fails none is stored.
func (d *DB) PutInboxItems(ctx context.Context, rows []InboxRow) error {
	if len(rows) == 0 {
		return nil
	}
	var b pgx.Batch
	for _, r := range rows {
		b.Queue(putInboxItemSQL, r.ID, r.Data, r.UpdatedAt, r.DoneAt)
	}
	return d.pool.SendBatch(ctx, &b).Close()
}

// DeleteInboxItems removes inbox items for good (a source the user took back).
func (d *DB) DeleteInboxItems(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM inbox_items WHERE id = ANY($1)`, ids)
	return err
}

// InboxItems returns the open items and those done after doneSince; older done items are deleted.
func (d *DB) InboxItems(ctx context.Context, doneSince time.Time) ([][]byte, error) {
	if _, err := d.pool.Exec(ctx, `DELETE FROM inbox_items WHERE done_at < $1`, doneSince); err != nil {
		return nil, err
	}
	rows, err := d.pool.Query(ctx, `SELECT data FROM inbox_items ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}

// ---- device tokens ----

// DeviceToken is one device's credential; the token itself is never stored, only its hash.
type DeviceToken struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	// MachineID binds a jarvis-link credential (kind link) to its machine in hub.json; empty for every other kind.
	MachineID string `json:"machine_id,omitempty"`
}

func (d *DB) CreateDeviceToken(ctx context.Context, t DeviceToken, hash string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash, machine_id)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`, t.ID, t.Name, t.Kind, t.Role, hash, t.MachineID)
	return err
}

// DeviceTokenByHash finds a token that is not revoked; nil when there is none.
func (d *DB) DeviceTokenByHash(ctx context.Context, hash string) (*DeviceToken, error) {
	var t DeviceToken
	err := d.pool.QueryRow(ctx, `SELECT id, name, kind, role, created_at, last_seen, COALESCE(machine_id, '') FROM device_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL`, hash).Scan(&t.ID, &t.Name, &t.Kind, &t.Role, &t.CreatedAt, &t.LastSeen, &t.MachineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) DeviceTokens(ctx context.Context) ([]DeviceToken, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, name, kind, role, created_at, last_seen, revoked_at, COALESCE(machine_id, '')
		FROM device_tokens WHERE kind <> $1 ORDER BY revoked_at IS NOT NULL, created_at DESC`, tombstoneKind)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DeviceToken, error) {
		var t DeviceToken
		err := r.Scan(&t.ID, &t.Name, &t.Kind, &t.Role, &t.CreatedAt, &t.LastSeen, &t.RevokedAt, &t.MachineID)
		return t, err
	})
}

// RevokeDeviceToken reports whether a live token was revoked.
func (d *DB) RevokeDeviceToken(ctx context.Context, id string) (bool, error) {
	var revoked bool
	err := pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE device_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
		if err != nil {
			return err
		}
		revoked = tag.RowsAffected() > 0
		if revoked {
			err = dropDeviceRows(ctx, tx, id)
		}
		return err
	})
	return revoked, err
}

// dropDeviceRows invalidates its pairing credentials and removes what a revoked credential registered.
func dropDeviceRows(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `UPDATE pair_tokens SET expires_at = now() WHERE device_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE credential_id = $1 OR device_id = $1`, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM phone_tools WHERE credential_id = $1 OR device_id = $1`, id)
	return err
}

func (d *DB) TouchDeviceToken(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `UPDATE device_tokens SET last_seen = now() WHERE id = $1`, id)
	return err
}

// TruncateForTest clears tables in tests.
func (d *DB) TruncateForTest(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `TRUNCATE workspace_board, workspace_snapshots, devices, device_tokens, live_activities, phone_tools, node_sessions, mail_accounts, interactions, settings, inbox_items, kb_entities, connector_secrets, machine_systems, machine_metrics, sent_messages, todos, todo_agents, assistant_always CASCADE`)
	return err
}
