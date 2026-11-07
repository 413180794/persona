// Package db is the hub's Postgres store (docker container persona-postgres on the hub machine, see docs/server.md).
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nwverse/persona/server/internal/proto"
)

// The migrations are two sets (docs/server.md "Migrations"): the hub's tables in migrations/hub, the manager's in
// migrations/manager. A hub's database runs the hub set, a manager's the manager set; while the default manager
// shares the hub's database, both run there, each recording its own part.
//
//go:embed migrations/hub/*.sql migrations/manager/*.sql
var migrations embed.FS

// Sets says which migrations a database gets (and so which side's tables it has).
type Sets uint8

const (
	// HubSet is the hub's own tables: connections, devices and their tokens, pairing, push delivery, peers and the
	// mesh, the admin page, the managers' roster and bus identities.
	HubSet Sets = 1 << iota
	// ManagerSet is a manager's: the assistant, inbox, knowledge base, todos, boards, extensions, the computers'
	// sessions and metrics.
	ManagerSet
	// AllSets is both, the hub's first: a test that uses either side's tables.
	AllSets = HubSet | ManagerSet
)

// setDirs are the sets' directories under migrations/, in the order they run.
var setDirs = []struct {
	set  Sets
	part string // what schema_migrations.part records
}{{HubSet, "hub"}, {ManagerSet, "manager"}}

type DB struct {
	pool *pgxpool.Pool
	sets Sets
	// olderHub: the hub's database is shared with the default manager's settings table, which a hub build from before
	// hub_settings reads (followOlderHub): the hub's keys are kept there too.
	olderHub  atomic.Bool
	olderMu   sync.Mutex // one syncOlderHub at a time
	closeOnce sync.Once
	closeDone chan struct{}
	// pids are the server processes of this pool's connections (OtherManagers tells them from another process's).
	pids *pidSet
}

// pidSet is the backend process ids of a pool's open connections.
type pidSet struct {
	mu sync.Mutex
	m  map[uint32]bool
}

func (p *pidSet) add(pid uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil {
		p.m = map[uint32]bool{}
	}
	p.m[pid] = true
}

func (p *pidSet) remove(pid uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, pid)
}

func (p *pidSet) list() []int32 {
	if p == nil {
		return []int32{} // not NULL: pid = ANY(NULL) would match nothing and hide every connection
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]int32, 0, len(p.m))
	for pid := range p.m {
		out = append(out, int32(pid))
	}
	return out
}

// Open connects and applies the pending migrations of sets. It retries for a while so the hub can start alongside
// the container.
func Open(ctx context.Context, url string, sets Sets) (*DB, error) {
	if sets == 0 || sets&^AllSets != 0 {
		return nil, fmt.Errorf("db.Open: bad migration sets %d", sets)
	}
	pids := &pidSet{}
	pool, err := connectTracked(ctx, url, appName(sets), pids)
	if err != nil {
		return nil, err
	}
	d := &DB{pool: pool, sets: sets, pids: pids}
	if err := d.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if sets == HubSet {
		if err := d.followOlderHub(ctx); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return d, nil
}

// Attach connects the way Open does for sets, migrating nothing and taking up nothing (a tool that looks at a database
// another build owns: assistant drop-shared-tables).
func Attach(ctx context.Context, url string, sets Sets) (*DB, error) {
	pids := &pidSet{}
	pool, err := connectTracked(ctx, url, appName(sets), pids)
	if err != nil {
		return nil, err
	}
	return &DB{pool: pool, sets: sets, pids: pids}, nil
}

// Check connects the way Open does and lets go again, migrating nothing: a new hub binary waiting to take over
// (hubedge, a standby slot) checks it can reach the database while the one serving still owns the schema.
func Check(ctx context.Context, url string) error {
	pool, err := connect(ctx, url, appName(HubSet))
	if err != nil {
		return err
	}
	pool.Close()
	return nil
}

// appName is the application_name a part's connections give Postgres, so a part can tell the other's connections
// (and ones of a build from before, which give none) on a database they share (managerdb.go).
func appName(sets Sets) string {
	switch sets {
	case HubSet:
		return AppHub
	case ManagerSet:
		return AppManager
	}
	return "persona"
}

// AppHub and AppManager are the application_name of the hub's connections and a manager's.
const (
	AppHub     = "persona-hub"
	AppManager = "persona-manager"
)

func connect(ctx context.Context, url, app string) (*pgxpool.Pool, error) {
	return connectTracked(ctx, url, app, nil)
}

// connectTracked is connect, keeping the server process id of each connection in pids while it is open (nil: none).
func connectTracked(ctx context.Context, url, app string, pids *pidSet) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New("hub 的数据库还没装好。运行 personad hub setup，它会帮你建好。")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database_url: %w", err)
	}
	if _, set := cfg.ConnConfig.RuntimeParams["application_name"]; !set && app != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = app
	}
	cfg.MaxConns = 8
	cfg.AfterConnect = func(_ context.Context, c *pgx.Conn) error {
		utcTimes(c.TypeMap())
		if pids != nil {
			pids.add(c.PgConn().PID())
		}
		return nil
	}
	if pids != nil {
		cfg.BeforeClose = func(c *pgx.Conn) { pids.remove(c.PgConn().PID()) }
	}
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
	return pool, nil
}

// utcTimes makes m read timestamptz values in UTC. pgx reads them in time.Local by default, which puts the hub
// machine's offset ("+08:00") into every time the protocol sends, where docs/protocol.md has UTC.
func utcTimes(m *pgtype.Map) {
	m.RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
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

// migrate runs the pending migrations of the database's sets, in order, each in its own transaction, recording them
// in schema_migrations.
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
	{"0428_todo_source.sql", "0429_todo_source.sql"},             // 0428 is manager_moved
}

// lastUnified is the last migration from before the split: a NULL-part row up to it stands for both sets.
const lastUnified = 419

// migrateFrom is migrate over the migrations/hub and migrations/manager directories of fsys (tests start from a
// database at an older state).
//
// schema_migrations has a row per migration a part applied: part is "hub" or "manager", NULL for the rows from before
// the split, when one set ran everything (such a row stands for both parts). A version is done for a set when it has a
// row of that part, or a NULL one from up to lastUnified (or, after it, of the same file: a number both sets use for
// different files, migrations_test.go sharedNumbers, is not taken as done by the other's row from a build before the
// split). So a database migrated before the split runs nothing again, and the few early migrations that are in both
// sets (they made tables for both sides; migrations_test.go bothSets lists them) run once per part on a database the
// hub and the default manager share.
func (d *DB) migrateFrom(ctx context.Context, fsys fs.FS) error {
	// Serialize concurrent hubs, managers (and tests) on one advisory lock.
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(424242)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(424242)`) //nolint:errcheck
	if err := splitSchemaMigrations(ctx, conn); err != nil {
		return err
	}

	for _, r := range renumbered {
		if err := renumberMigration(ctx, conn, r.from, r.to); err != nil {
			return fmt.Errorf("renumber migration %s: %w", r.from, err)
		}
	}
	for _, sd := range setDirs {
		if d.sets&sd.set == 0 {
			continue
		}
		if err := migrateSet(ctx, conn, fsys, sd.part); err != nil {
			return err
		}
	}
	return nil
}

// splitSchemaMigrations makes schema_migrations, or gives one from before the split its part column: the rows it has
// keep part NULL, and a version may have a row for each part.
func splitSchemaMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version int NOT NULL, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now(), part text)`,
		`ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS part text`,
		`ALTER TABLE schema_migrations DROP CONSTRAINT IF EXISTS schema_migrations_pkey`,
		`CREATE UNIQUE INDEX IF NOT EXISTS schema_migrations_version_part ON schema_migrations (version, (coalesce(part, '')))`,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("schema_migrations: %w", err)
		}
	}
	return nil
}

// migrateSet applies the migrations in migrations/<part> that part has not.
func migrateSet(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, part string) error {
	dir := "migrations/" + part
	entries, err := fs.ReadDir(fsys, dir)
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
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations
			WHERE version = $1 AND (part = $2 OR part IS NULL AND (version <= $3 OR name = $4)))`, version, part, lastUnified, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name, part) VALUES ($1, $2, $3)`, version, name, part)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s/%s: %w", part, name, err)
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
	// RelayError: why the phone got no grant from its relay, in its own words (the relay's status and answer, a
	// redirect, a network error); empty when it got one or names no relay.
	RelayError string `json:"relay_error,omitempty"`
	// Features: what the app on it understands beyond the first version's pushes ("withdraw": a background push
	// naming the cards that are over, hub/push_withdraw.go). The hub keeps only names it knows.
	Features []string `json:"features,omitempty"`
	// Locale: the interface language it registered with (zh-Hans / en / ja, docs/i18n.md); "" is the user's.
	Locale string `json:"locale,omitempty"`
	// Credential: the device token (device_tokens.id) that registered it, set by the hub, never from the request;
	// empty for the pairing token. Revoking it forgets the device (hub: Pusher.signedOut).
	Credential string    `json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
	// Prefs is the device's notification settings as JSON (hub decodes it); empty = defaults. Registration
	// (UpsertDevice) never touches it, only SetDevicePrefs does.
	Prefs json.RawMessage `json:"-"`
}

func (d *DB) UpsertDevice(ctx context.Context, v Device) error {
	features := v.Features
	if features == nil {
		features = []string{} // the column is NOT NULL
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, push_key, relay_url, relay_grant, credential, updated_at, credential_id, locale, relay_error, features)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), $11, $12, $13, $14)
		ON CONFLICT (device_id) DO UPDATE SET name = $2, kind = $3, apns_token = $4, apns_env = $5, topic = $6,
			push_key = $7, relay_url = $8, relay_grant = $9, credential = $10, updated_at = now(), credential_id = $11, locale = $12, relay_error = $13, features = $14`,
		v.DeviceID, v.Name, v.Kind, v.APNSToken, v.APNSEnv, v.Topic, v.PushKey, v.RelayURL, v.RelayGrant, v.Credential, v.CredentialID, v.Locale, v.RelayError, features)
	return err
}

func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, name, kind, apns_token, apns_env, topic, push_key, relay_url, relay_grant, credential, updated_at, prefs, credential_id, locale, relay_error, features FROM devices ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var v Device
		var prefs []byte
		err := r.Scan(&v.DeviceID, &v.Name, &v.Kind, &v.APNSToken, &v.APNSEnv, &v.Topic, &v.PushKey, &v.RelayURL, &v.RelayGrant, &v.Credential, &v.UpdatedAt, &prefs, &v.CredentialID, &v.Locale, &v.RelayError, &v.Features)
		if len(prefs) > 0 {
			v.Prefs = prefs
		}
		return v, err
	})
}

// SetDevicePrefs stores a device's notification settings. The device may not have registered for push yet (no
// permission, the simulator, a browser, which never does): the row is created then and filled in when it does.
// credential is the device token that wrote them: a row without one takes it, so revoking that token (Pusher.signedOut)
// also removes a row only prefs ever made; registering sets it as usual.
func (d *DB) SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, updated_at, prefs, credential)
		VALUES ($1, '', '', '', '', '', now(), $2, $3)
		ON CONFLICT (device_id) DO UPDATE SET prefs = $2,
			credential = CASE WHEN devices.credential = '' THEN EXCLUDED.credential ELSE devices.credential END`, id, []byte(prefs), credential)
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
		// The DELETE and every INSERT go out as one batch: one round trip instead of 1+N while the hub holds
		// phoneToolsMu on each announcement (about 26 tools per app).
		var b pgx.Batch
		b.Queue(`DELETE FROM phone_tools WHERE device_id = $1`, registration.DeviceID)
		for i, t := range registration.Tools {
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{}`)
			}
			b.Queue(`INSERT INTO phone_tools (device_id, platform, credential_id, name, description, input_schema, position)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (device_id, name) DO UPDATE SET description = $5, input_schema = $6, position = $7, updated_at = now()`,
				registration.DeviceID, registration.Platform, registration.CredentialID, t.Name, t.Description, []byte(schema), i)
		}
		return tx.SendBatch(ctx, &b).Close()
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
// Nodes of #3782 also wrote ultracode observations of sessions the user started, as
// rows without a launcher or directory; newer nodes ignore those rows when loading
// and write no rows for sessions they did not start (#5510).
type NodeSession struct {
	Model            string    `json:"model,omitempty"`
	ModelFingerprint string    `json:"model_fingerprint,omitempty"`
	ModelEffort      string    `json:"model_effort,omitempty"`
	Pane             string    `json:"pane,omitempty"`
	Launcher         string    `json:"launcher"`
	Cwd              string    `json:"cwd"`
	StartedAt        time.Time `json:"started_at"`
	Tasks            []string  `json:"tasks,omitempty"` // an assistant conversation's running background tasks
	// The permission mode / effort the app set on an assistant conversation (node/modes.go); kept with the model.
	AppMode   string `json:"app_mode,omitempty"`
	AppEffort string `json:"app_effort,omitempty"`
	// Opaque node-owned process observation, not a preference for later starts.
	UltracodeProcess json.RawMessage `json:"ultracode_process,omitempty"`
}

func (d *DB) PutNodeSession(ctx context.Context, machine, id string, s NodeSession) error {
	// The table needs a timestamp for retention, but a row stored without a start time
	// next to an ultracode observation returns without one: the node tells the rows
	// #3782 wrote for sessions the user started by that (node.observationOnly).
	unknownStart := s.StartedAt.IsZero() && len(s.UltracodeProcess) > 0
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
		jsonb_build_object('model', $8::text, 'model_fingerprint', $9::text, 'model_effort', $10::text,
			'app_mode', $11::text, 'app_effort', $12::text, 'ultracode_process', $13::jsonb,
			'started_at_unknown', $14::bool), now() FROM saved
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		machine, id, s.Pane, s.Launcher, s.Cwd, s.StartedAt, tasks, s.Model, s.ModelFingerprint, s.ModelEffort, s.AppMode, s.AppEffort,
		s.UltracodeProcess, unknownStart)
	return err
}

// nodeSessionsSQL reads a machine's sessions from the last 60 days through node_sessions_started (0418), with the
// model settings PutNodeSession keeps beside each one.
// settings holds a node_model row for every session ever started, so the row is fetched per session through
// settings_pkey: a join on the computed key lets the planner hash all of settings instead. The jsonb comes back
// whole because extracting its fields in an outer SELECT repeats the subquery once per field.
const nodeSessionsSQL = `SELECT n.session_id, n.pane, n.launcher, n.cwd, n.started_at, n.tasks,
		COALESCE((SELECT s.value FROM settings s WHERE s.key = 'node_model.' || n.machine_id || '.' || n.session_id), '{}')
		FROM node_sessions n
		WHERE n.machine_id = $1 AND n.started_at > now() - interval '60 days'`

// nodeModel is the node_model.<machine>.<session> settings value PutNodeSession writes.
type nodeModel struct {
	Model            string `json:"model"`
	ModelFingerprint string `json:"model_fingerprint"`
	ModelEffort      string `json:"model_effort"`
	AppMode          string `json:"app_mode"`
	AppEffort        string `json:"app_effort"`
	// UltracodeProcess and StartedAtUnknown: the ultracode observation of the session's process, and a row stored
	// without a start time (see PutNodeSession).
	UltracodeProcess json.RawMessage `json:"ultracode_process"`
	StartedAtUnknown bool            `json:"started_at_unknown"`
}

// NodeSessions returns a machine's sessions from the last 60 days.
func (d *DB) NodeSessions(ctx context.Context, machine string) (map[string]NodeSession, error) {
	rows, err := d.pool.Query(ctx, nodeSessionsSQL, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]NodeSession{}
	for rows.Next() {
		var id string
		var s NodeSession
		var m nodeModel
		if err := rows.Scan(&id, &s.Pane, &s.Launcher, &s.Cwd, &s.StartedAt, &s.Tasks, &m); err != nil {
			return nil, err
		}
		s.Model, s.ModelFingerprint, s.ModelEffort, s.AppMode, s.AppEffort = m.Model, m.ModelFingerprint, m.ModelEffort, m.AppMode, m.AppEffort
		if len(m.UltracodeProcess) > 0 && string(m.UltracodeProcess) != "null" {
			s.UltracodeProcess = m.UltracodeProcess
		}
		if m.StartedAtUnknown {
			s.StartedAt = time.Time{}
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

// The statements of Interactions, one per combination of filters. The single statement they replace matched an
// empty filter with an OR ("$1 = empty OR machine_id = $1"). Once Postgres switched that cached statement to a generic
// plan (after five runs) it could not fold the empty test away, so it walked interactions_created over every row ever
// kept. TestInteractionsHistoryIndexed checks the plans of the cached ones.
const (
	interactionsSQL        = `SELECT payload, status, resolution, resolved_at FROM interactions ORDER BY created_at DESC LIMIT $1`
	interactionsMachineSQL = `SELECT payload, status, resolution, resolved_at FROM interactions
		WHERE machine_id = $1 ORDER BY created_at DESC LIMIT $2`
	interactionsSessionSQL = `SELECT payload, status, resolution, resolved_at FROM interactions
		WHERE session_id = $1 ORDER BY created_at DESC LIMIT $2`
	interactionsMachineSessionSQL = `SELECT payload, status, resolution, resolved_at FROM interactions
		WHERE machine_id = $1 AND session_id = $2 ORDER BY created_at DESC LIMIT $3`
)

// Interactions lists recent interactions, newest first, optionally for one machine and/or one session.
func (d *DB) Interactions(ctx context.Context, machine, session string, limit int) ([]InteractionRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// A machine alone or a session alone has no index that orders by created_at: the best plan walks
	// interactions_created for a busy one and reads interactions_session then sorts for a quiet one. A generic plan
	// settles on the walk, which reads every row ever kept for a quiet one, so these two go as the unnamed statement
	// of QueryExecModeExec, planned for each call with its values. TestInteractionsNotCached checks that.
	var q string
	var args []any
	switch {
	case machine != "" && session != "":
		q, args = interactionsMachineSessionSQL, []any{machine, session, limit}
	case machine != "":
		q, args = interactionsMachineSQL, []any{pgx.QueryExecModeExec, machine, limit}
	case session != "":
		q, args = interactionsSessionSQL, []any{pgx.QueryExecModeExec, session, limit}
	default:
		q, args = interactionsSQL, []any{limit}
	}
	rows, err := d.pool.Query(ctx, q, args...)
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

// InteractionRecord is one interaction with its outcome; nil when there is no such row.
func (d *DB) InteractionRecord(ctx context.Context, id string) (*InteractionRecord, error) {
	var rec InteractionRecord
	var payload, res []byte
	err := d.pool.QueryRow(ctx, `SELECT payload, status, resolution, resolved_at FROM interactions WHERE id = $1`, id).
		Scan(&payload, &rec.Status, &res, &rec.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payload, &rec.Interaction); err != nil {
		return nil, err
	}
	if len(res) > 0 {
		rec.Resolution = &proto.Resolution{}
		if err := json.Unmarshal(res, rec.Resolution); err != nil {
			return nil, err
		}
	}
	return &rec, nil
}

// The startup statements of ExpireStaleInteractions. Both must stay answerable from the interactions_pending
// partial index (migration 0393); TestPendingInteractionsIndexed checks the plans of these exact constants.
const (
	resumablePendingSQL = `SELECT payload FROM interactions WHERE status = 'pending'
		AND (payload->>'resumable')::boolean IS TRUE AND (payload->>'expires_at')::timestamptz > now()`
	cancelPendingSQL = `UPDATE interactions SET status = 'cancelled', resolved_at = now()
		WHERE status = 'pending' AND NOT (id = ANY($1))
		AND id NOT IN (SELECT interaction_id FROM session_work WHERE interaction_id <> '')`
)

// ExpireStaleInteractions marks interactions left pending by a previous hub process cancelled, and returns the
// resumable ones not yet expired instead: their hooks reconnect and claim them again. The manager's cards about a
// turn (session_work's interaction_id) stay pending too: it puts them back up under the same id from session_work, and
// ends the ones it does not (assistant/agents_work.go loadWork).
func (d *DB) ExpireStaleInteractions(ctx context.Context) ([]proto.Interaction, error) {
	rows, err := d.pool.Query(ctx, resumablePendingSQL)
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
	_, err = d.pool.Exec(ctx, cancelPendingSQL, ids)
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

// ---- "Always allow in this conversation" on assistant conversations (assistant_always) ----

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

// Each side has its own settings (docs/server.md "Migrations"): the hub's in hub_settings, a manager's in settings.
// A database opened for the hub alone reads and writes the hub's; one opened for a manager (or for both, in tests)
// the manager's. Neither side reads the other's keys: what one needs of the other comes over the bus.

// hubKeys are the hub's settings keys from before hub_settings (migrations/hub/0421_hub_settings.sql copies them
// once), and hubKeyPrefixes the families of them. On a database shared with the default manager they are kept in
// settings too, for a hub build from before: every write of one goes through PutSetting / TakeSetting (CompareSetting
// does not keep them there; the hub uses it for none).
var (
	hubKeys = []string{"hub_id", "user_locale", "look", "hub_update", "hub.event_log", "hub.assistant_profile",
		"hub.extensions_report", "managers.default", "managers.machines", "sessions.deleted", "persona_tiers", "pins",
		"mutes", "mesh.epoch", "mesh.records"}
	hubKeyPrefixes = []string{"codex_inventory/"}
)

// isHubKey: key is one of the hub's from before hub_settings.
func isHubKey(key string) bool {
	if slices.Contains(hubKeys, key) {
		return true
	}
	for _, p := range hubKeyPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// keepForOlderHub writes (raw nil: deletes) a hub key in the shared settings table as well, in tx with the write to
// hub_settings (one now(): the same updated_at), so a hub build from before hub_settings that is switched back to
// starts from the values in effect, and followOlderHub then takes only what that build changed after.
func (d *DB) keepForOlderHub(ctx context.Context, tx pgx.Tx, key string, raw []byte) error {
	if raw == nil {
		_, err := tx.Exec(ctx, `DELETE FROM settings WHERE key = $1`, key)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`, key, raw)
	return err
}

// followOlderHub takes up what a hub build from before hub_settings wrote in the settings table it shares with the
// default manager, at every start of the hub: a switch back to such a build (blue-green, docs/hub-restart.md) and
// forward again keeps the changes made meanwhile. A key it wrote later than this build is taken, its event log
// hand-over too; taking a hand-over removes it from both tables (a hand-over is read once: left there, a later switch
// back would resume a stale stream).
func (d *DB) followOlderHub(ctx context.Context) error {
	_, err := d.sharedWithOlderHub(ctx)
	return err
}

// sharedWithOlderHub: the hub's database has the default manager's settings table, where a hub build from before
// hub_settings reads and writes the hub's keys. It is looked for until it is there (on a fresh install the hub
// opens the database before its default manager makes that table); the first time it is, that build's later writes
// are taken up and the values in effect are written there too (syncOlderHub).
func (d *DB) sharedWithOlderHub(ctx context.Context) (bool, error) {
	if d.sets != HubSet {
		return false, nil
	}
	if d.olderHub.Load() {
		return true, nil
	}
	var shared bool
	if err := d.pool.QueryRow(ctx, `SELECT to_regclass('settings') IS NOT NULL`).Scan(&shared); err != nil || !shared {
		return false, err
	}
	d.olderMu.Lock()
	defer d.olderMu.Unlock()
	if d.olderHub.Load() {
		return true, nil
	}
	if err := d.syncOlderHub(ctx); err != nil {
		if d.olderHubGone(err) {
			return false, nil // dropped since it was looked for
		}
		return false, err
	}
	d.olderHub.Store(true)
	return true, nil
}

// syncOlderHub, in one transaction: takes up what an older build wrote to settings later than this one did (its
// event log hand-over too), drops this build's own hand-over that an older build took meanwhile (a hand-over is read
// once: resumed again, it would replay a stream that went on without it), and writes the hub's other values in effect
// to settings, so an older build switched back to starts from them. The shared table is locked first, as every write of
// a hub key starts there (keepForOlderHub, then hub_settings): one lock order, rows that are not there yet included.
func (d *DB) syncOlderHub(ctx context.Context) error {
	var like []string
	for _, p := range hubKeyPrefixes {
		like = append(like, strings.ReplaceAll(p, "_", `\_`)+"%")
	}
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE settings IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("hub settings: lock the shared table: %w", err)
		}
		// Since when this database's hub keys are kept in settings too: only a hand-over written from then on was there
		// for an older build to take.
		if _, err := tx.Exec(ctx, `INSERT INTO hub_settings (key, value, updated_at) VALUES ($1, 'true', now())
			ON CONFLICT (key) DO NOTHING`, mirroredSinceKey); err != nil {
			return fmt.Errorf("hub settings: %s: %w", mirroredSinceKey, err)
		}
		// Written to both tables when this build stopped (keepForOlderHub), gone from settings: an older build served in
		// between and took it. Taking it removes both (TakeSetting); until then both copies stay.
		if _, err := tx.Exec(ctx, `DELETE FROM hub_settings h WHERE h.key = 'hub.event_log'
			AND NOT EXISTS (SELECT 1 FROM settings WHERE key = 'hub.event_log')
			AND h.updated_at >= (SELECT updated_at FROM hub_settings WHERE key = $1)`, mirroredSinceKey); err != nil {
			return fmt.Errorf("hub settings: the event log an older build took: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO hub_settings AS h (key, value, updated_at)
			SELECT key, value, updated_at FROM settings WHERE key = ANY($1) OR key LIKE ANY($2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
			WHERE h.updated_at < EXCLUDED.updated_at`, hubKeys, like); err != nil {
			return fmt.Errorf("hub settings: what an older build wrote: %w", err)
		}
		// Not the event log: a hand-over only in hub_settings was never there for an older build (written before
		// settings was), and stays this build's to take (loadEventLog).
		if _, err := tx.Exec(ctx, `INSERT INTO settings AS s (key, value, updated_at)
			SELECT key, value, updated_at FROM hub_settings WHERE (key = ANY($1) OR key LIKE ANY($2)) AND key <> 'hub.event_log'
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
			WHERE s.updated_at < EXCLUDED.updated_at`, hubKeys, like); err != nil {
			return fmt.Errorf("hub settings: kept for an older build: %w", err)
		}
		return nil
	})
}

// mirroredSinceKey is the hub's setting whose updated_at is when it began keeping its keys in the shared settings
// table too (syncOlderHub's first run on the database).
const mirroredSinceKey = "hub.settings_mirrored_since"

// olderHubGone: err is the shared settings table missing (dropped since it was found): the hub's keys are no longer
// kept there, and the write is tried again on hub_settings alone.
func (d *DB) olderHubGone(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42P01" && d.sets == HubSet {
		d.olderHub.Store(false)
		return true
	}
	return false
}

// settingsTable is the table Setting and the rest use: hub_settings for the hub's database, settings otherwise.
func (d *DB) settingsTable() string {
	if d.sets == HubSet {
		return "hub_settings"
	}
	return "settings"
}

// Setting decodes the value stored under key into out; it reports false when the key is unset.
func (d *DB) Setting(ctx context.Context, key string, out any) (bool, error) {
	var raw []byte
	err := d.pool.QueryRow(ctx, `SELECT value FROM `+d.settingsTable()+` WHERE key = $1`, key).Scan(&raw)
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
	put := func(q execer) error {
		_, err := q.Exec(ctx, `INSERT INTO `+d.settingsTable()+` (key, value, updated_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`, key, raw)
		return err
	}
	if !isHubKey(key) {
		return put(d.pool)
	}
	if shared, err := d.sharedWithOlderHub(ctx); err != nil || !shared {
		if err != nil {
			return err
		}
		return put(d.pool)
	}
	err = pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := d.keepForOlderHub(ctx, tx, key, raw); err != nil { // the shared table first: one lock order
			return err
		}
		return put(tx)
	})
	if d.olderHubGone(err) {
		return put(d.pool)
	}
	return err
}

// execer is a pool or a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TakeSetting is Setting that also removes the key, in one statement: a value meant to be read once (the event log a
// stopping hub hands to the next one, docs/hub-restart.md) is never read twice.
func (d *DB) TakeSetting(ctx context.Context, key string, out any) (bool, error) {
	var raw []byte
	shared := false
	if isHubKey(key) {
		var err error
		if shared, err = d.sharedWithOlderHub(ctx); err != nil {
			return false, err
		}
	}
	take := func(shared bool) error {
		return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
			if shared { // taken: not there for an older build either (the shared table first: one lock order)
				if err := d.keepForOlderHub(ctx, tx, key, nil); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctx, `DELETE FROM `+d.settingsTable()+` WHERE key = $1 RETURNING value`, key).Scan(&raw)
		})
	}
	err := take(shared)
	if shared && d.olderHubGone(err) {
		err = take(false)
	}
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
	table := d.settingsTable()
	tag, err := d.pool.Exec(ctx, `INSERT INTO `+table+` AS s (key, value, updated_at)
		SELECT $1, $2::jsonb, now() WHERE $3::bigint = 0
		ON CONFLICT (key) DO UPDATE SET value = $2::jsonb, updated_at = now()
		WHERE COALESCE((s.value->>'revision')::bigint, 0) = $3`, key, raw, revision)
	if revision != 0 && err == nil && tag.RowsAffected() == 0 {
		tag, err = d.pool.Exec(ctx, `UPDATE `+table+` SET value = $2::jsonb, updated_at = now()
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

// seenTimesSQL reads the recent views through session_views_seen; the table keeps every session ever viewed.
const seenTimesSQL = `SELECT machine_id, session_id, seen_at FROM session_views WHERE seen_at > now() - interval '30 days'`

// SeenTimes returns when each session was last looked at (last 30 days).
func (d *DB) SeenTimes(ctx context.Context) (map[[2]string]time.Time, error) {
	rows, err := d.pool.Query(ctx, seenTimesSQL)
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
	// A bare pgx.Batch is only a pipeline of autocommit statements, so a connection failure midway can leave some
	// rows stored; the explicit transaction makes the all-or-nothing contract real.
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var b pgx.Batch
	for _, r := range rows {
		b.Queue(putInboxItemSQL, r.ID, r.Data, r.UpdatedAt, r.DoneAt)
	}
	if err := tx.SendBatch(ctx, &b).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	// MachineID binds a persona-link credential (kind link) to its machine in hub.json; empty for every other kind.
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

// TruncateForTest clears tables in tests: those of them the database's sets have.
func (d *DB) TruncateForTest(ctx context.Context) error {
	var tables []string
	err := d.pool.QueryRow(ctx, `SELECT coalesce(array_agg(t), '{}') FROM unnest($1::text[]) AS t WHERE to_regclass(t) IS NOT NULL`,
		[]string{"workspace_board", "workspace_snapshots", "devices", "device_tokens", "live_activities", "phone_tools",
			"node_sessions", "mail_accounts", "interactions", "settings", "inbox_items", "kb_entities", "connector_secrets",
			"machine_systems", "machine_metrics", "sent_messages", "todos", "todo_agents", "assistant_always", "machine_trust", "hub_settings",
			"manager_outbox", "handed_over",
			"machine_audit", "sent_as_user"}).Scan(&tables)
	if err != nil || len(tables) == 0 {
		return err
	}
	_, err = d.pool.Exec(ctx, `TRUNCATE `+strings.Join(tables, ", ")+` CASCADE`)
	return err
}
