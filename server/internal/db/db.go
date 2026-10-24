// Package db is the hub's Postgres store (docker container jarvis-postgres on example-machine, see docs/server.md).
package db

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chyroc/jarvis/server/internal/proto"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	pool *pgxpool.Pool
}

// Open connects and applies pending migrations. It retries for a while so the hub can start alongside the container.
func Open(ctx context.Context, url string) (*DB, error) {
	if url == "" {
		return nil, errors.New("database_url is empty (run scripts/deploy.sh example-machine to create the Postgres container)")
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

func (d *DB) Close() { d.pool.Close() }

// migrate runs migrations/NNNN_*.sql in order, each in its own transaction, recording them in schema_migrations.
func (d *DB) migrate(ctx context.Context) error {
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

	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %s", name)
		}
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile("migrations/" + name)
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

// ---- devices ----

type Device struct {
	DeviceID  string    `json:"device_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	APNSToken string    `json:"apns_token,omitempty"`
	APNSEnv   string    `json:"apns_env,omitempty"`
	Topic     string    `json:"topic,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d *DB) UpsertDevice(ctx context.Context, v Device) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (device_id) DO UPDATE SET name = $2, kind = $3, apns_token = $4, apns_env = $5, topic = $6, updated_at = now()`,
		v.DeviceID, v.Name, v.Kind, v.APNSToken, v.APNSEnv, v.Topic)
	return err
}

func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, name, kind, apns_token, apns_env, topic, updated_at FROM devices ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var v Device
		err := r.Scan(&v.DeviceID, &v.Name, &v.Kind, &v.APNSToken, &v.APNSEnv, &v.Topic, &v.UpdatedAt)
		return v, err
	})
}

// DeleteDevice forgets a device (e.g. APNs says the token is no longer valid).
func (d *DB) DeleteDevice(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM devices WHERE device_id = $1`, id)
	return err
}

// ---- phone tools ----

// ReplacePhoneTools stores the iPhone's current tool list, dropping tools it no longer offers.
func (d *DB) ReplacePhoneTools(ctx context.Context, tools []proto.Tool) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM phone_tools`); err != nil {
			return err
		}
		for i, t := range tools {
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO phone_tools (name, description, input_schema, position) VALUES ($1, $2, $3, $4)
				ON CONFLICT (name) DO UPDATE SET description = $2, input_schema = $3, position = $4, updated_at = now()`,
				t.Name, t.Description, []byte(schema), i); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) PhoneTools(ctx context.Context) ([]proto.Tool, error) {
	rows, err := d.pool.Query(ctx, `SELECT name, description, input_schema FROM phone_tools ORDER BY position, name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (proto.Tool, error) {
		var t proto.Tool
		var schema []byte
		err := r.Scan(&t.Name, &t.Description, &schema)
		t.InputSchema = schema
		return t, err
	})
}

// ---- node sessions ----

// NodeSession is what a node remembers about a session it started (see internal/node).
type NodeSession struct {
	Pane      string    `json:"pane,omitempty"`
	Launcher  string    `json:"launcher"`
	Cwd       string    `json:"cwd"`
	StartedAt time.Time `json:"started_at"`
}

func (d *DB) PutNodeSession(ctx context.Context, machine, id string, s NodeSession) error {
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO node_sessions (machine_id, session_id, pane, launcher, cwd, started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (machine_id, session_id) DO UPDATE SET pane = $3, launcher = $4, cwd = $5, started_at = $6, updated_at = now()`,
		machine, id, s.Pane, s.Launcher, s.Cwd, s.StartedAt)
	return err
}

// NodeSessions returns a machine's sessions from the last 60 days.
func (d *DB) NodeSessions(ctx context.Context, machine string) (map[string]NodeSession, error) {
	rows, err := d.pool.Query(ctx, `SELECT session_id, pane, launcher, cwd, started_at FROM node_sessions
		WHERE machine_id = $1 AND started_at > now() - interval '60 days'`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]NodeSession{}
	for rows.Next() {
		var id string
		var s NodeSession
		if err := rows.Scan(&id, &s.Pane, &s.Launcher, &s.Cwd, &s.StartedAt); err != nil {
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

// ExpireStaleInteractions marks interactions left pending by a previous hub process (they died with it).
func (d *DB) ExpireStaleInteractions(ctx context.Context) error {
	_, err := d.pool.Exec(ctx, `UPDATE interactions SET status = 'cancelled', resolved_at = now() WHERE status = 'pending'`)
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
