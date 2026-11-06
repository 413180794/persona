package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/db/dbtest"
)

// bothSets are the migrations in both sets, frozen: from before the split, 0001 made tables for both sides (each set
// has its part), and sent_messages is a table each side keeps in its own database (the hub's sends from the apps, the
// manager's from the inbox). A new migration goes in exactly one set.
var bothSets = []string{"0001_init.sql", "0071_sent_messages.sql", "0241_sent_echo.sql"}

// sharedTables are the tables both sets make (a database the hub and the default manager share has one of each).
var sharedTables = []string{"sent_messages"}

var createTable = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?([a-z_]+)`)

// setFiles are each part's migration names, sorted.
func setFiles(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, sd := range setDirs {
		entries, err := fs.ReadDir(migrations, "migrations/"+sd.part)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			out[sd.part] = append(out[sd.part], e.Name())
		}
		sort.Strings(out[sd.part])
	}
	return out
}

// setTables are the tables a part's migrations make.
func setTables(t *testing.T, part string) []string {
	t.Helper()
	var out []string
	for _, name := range setFiles(t)[part] {
		b, err := fs.ReadFile(migrations, "migrations/"+part+"/"+name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range createTable.FindAllStringSubmatch(string(b), -1) {
			if !slices.Contains(out, strings.ToLower(m[1])) {
				out = append(out, strings.ToLower(m[1]))
			}
		}
	}
	sort.Strings(out)
	return out
}

// Every migration is in exactly one set (docs/server.md "Migrations"), but for the frozen few from before the split.
func TestMigrationSets(t *testing.T) {
	root, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range root {
		if !e.IsDir() || (e.Name() != "hub" && e.Name() != "manager") {
			t.Errorf("migrations/%s: a migration goes in migrations/hub (the hub's tables) or migrations/manager (a manager's)", e.Name())
		}
	}
	files := setFiles(t)
	versions := map[int]string{}
	for _, part := range []string{"hub", "manager"} {
		seen := map[int]bool{}
		for _, name := range files[part] {
			if !strings.HasSuffix(name, ".sql") {
				t.Errorf("migrations/%s/%s: not a .sql file", part, name)
				continue
			}
			v, err := migrationVersion(name)
			if err != nil {
				t.Error(err)
				continue
			}
			if seen[v] {
				t.Errorf("migrations/%s: two migrations numbered %d", part, v)
			}
			seen[v] = true
			if was, ok := versions[v]; ok && (was != name || !slices.Contains(bothSets, name)) {
				t.Errorf("migration %d is in both sets (%s, %s): a new migration goes in exactly one; take a number neither set has", v, was, name)
			}
			versions[v] = name
		}
	}
	for _, name := range bothSets {
		if !slices.Contains(files["hub"], name) || !slices.Contains(files["manager"], name) {
			t.Errorf("%s is listed in bothSets but is not in both sets", name)
		}
	}
	hub, manager := setTables(t, "hub"), setTables(t, "manager")
	var both []string
	for _, tb := range hub {
		if slices.Contains(manager, tb) {
			both = append(both, tb)
		}
	}
	if !slices.Equal(both, sharedTables) {
		t.Errorf("tables both sets make: %v, want %v: a table is one side's (docs/server.md \"Migrations\")", both, sharedTables)
	}
}

// freshDatabase is a new empty database next to the test one, dropped afterwards.
func freshDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv(dbtest.Env)
	if base == "" {
		t.Skip(dbtest.Env + " not set")
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "persona_split_t_" + hex.EncodeToString(b)
	url, err := WithDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := EnsureDatabase(ctx, url); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin, err := pgx.Connect(context.Background(), base)
		if err == nil {
			_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			_ = admin.Close(context.Background())
		}
	})
	return url
}

// tablesOf are the database's tables but schema_migrations, sorted.
func tablesOf(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.pool.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'schema_migrations' ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// records are schema_migrations' rows as "version name part", sorted.
func records(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.pool.Query(context.Background(), `SELECT version, name, coalesce(part, '-') FROM schema_migrations`)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var v int
		var name, part string
		if err := rows.Scan(&v, &name, &part); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{name, part}, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// A hub's new database has the hub's tables only, a manager's the manager's; one they share has both, each part
// recorded once.
func TestMigrateFreshSets(t *testing.T) {
	ctx := context.Background()
	hubTables, managerTables := setTables(t, "hub"), setTables(t, "manager")
	for _, c := range []struct {
		name   string
		sets   []Sets
		tables []string
		rows   int
	}{
		{"hub", []Sets{HubSet}, hubTables, len(setFiles(t)["hub"])},
		{"manager", []Sets{ManagerSet}, managerTables, len(setFiles(t)["manager"])},
		{"shared", []Sets{HubSet, ManagerSet, AllSets}, union(hubTables, managerTables),
			len(setFiles(t)["hub"]) + len(setFiles(t)["manager"])},
	} {
		t.Run(c.name, func(t *testing.T) {
			url := freshDatabase(t)
			var d *DB
			for _, s := range c.sets {
				var err error
				if d, err = Open(ctx, url, s); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(d.Close)
			}
			if got := tablesOf(t, d); !slices.Equal(got, c.tables) {
				t.Fatalf("tables %v, want %v", got, c.tables)
			}
			if got := records(t, d); len(got) != c.rows {
				t.Fatalf("%d records, want %d: %v", len(got), c.rows, got)
			}
		})
	}
}

// unifiedMigrate is the migrator from before the split: one schema_migrations row per version, every migration in
// order, up to version until (0: all).
func unifiedMigrate(t *testing.T, url string, until int) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	// The files as they were: 0001 was both parts in one; the others in both sets are the same in each.
	sql := map[string]string{}
	for _, part := range []string{"hub", "manager"} {
		for _, name := range setFiles(t)[part] {
			b, err := fs.ReadFile(migrations, "migrations/"+part+"/"+name)
			if err != nil {
				t.Fatal(err)
			}
			if was, ok := sql[name]; ok && was != string(b) {
				sql[name] = was + "\n" + string(b)
			} else {
				sql[name] = string(b)
			}
		}
	}
	names := make([]string, 0, len(sql))
	for name := range sql {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		v, _ := migrationVersion(name)
		if until > 0 && v > until {
			break
		}
		if _, err := conn.Exec(ctx, sql[name]); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, v, name); err != nil {
			t.Fatal(err)
		}
	}
}

// A database migrated before the split (the hub and its default manager on one) runs nothing again, whichever side
// opens it first; one left at an older state gets the rest of both sets; and a binary from before the split still
// reads and adds records.
func TestMigrateUpgradeFromUnified(t *testing.T) {
	ctx := context.Background()
	all := union(setTables(t, "hub"), setTables(t, "manager"))
	t.Run("complete", func(t *testing.T) {
		url := freshDatabase(t)
		unifiedMigrate(t, url, 0)
		probe, err := pgx.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer probe.Close(ctx)
		// A migration that ran again would fail on these (0001's CREATE TABLE has no IF NOT EXISTS) or change them.
		if _, err := probe.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('kept', '"yes"')`); err != nil {
			t.Fatal(err)
		}
		var before []string
		for i, s := range []Sets{ManagerSet, HubSet, AllSets} {
			d, err := Open(ctx, url, s)
			if err != nil {
				t.Fatalf("open %d: %v", s, err)
			}
			got := records(t, d)
			if i == 0 {
				before = got
				for _, r := range got {
					if !strings.HasSuffix(r, " -") {
						t.Fatalf("a record from before the split changed or was added: %s", r)
					}
				}
			} else if !slices.Equal(got, before) {
				t.Fatalf("records after opening with %d: %v, want %v", s, got, before)
			}
			if tables := tablesOf(t, d); !slices.Equal(tables, all) {
				t.Fatalf("tables %v, want %v", tables, all)
			}
			var v string
			if ok, err := d.Setting(ctx, "kept", &v); s != HubSet && (err != nil || !ok || v != "yes") { // the manager's settings
				t.Fatalf("setting kept = %q %v %v", v, ok, err)
			}
			d.Close()
		}
		if len(before) != len(setFiles(t)["hub"])+len(setFiles(t)["manager"])-len(bothSets) {
			t.Fatalf("%d records: %v", len(before), before)
		}
		// What a binary from before the split does with it (a hub switched back to an older slot): its lookup and its
		// insert still work.
		var done bool
		if err := probe.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, 1).Scan(&done); err != nil || !done {
			t.Fatalf("old lookup: %v %v", done, err)
		}
		if _, err := probe.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, 99999, "99999_later.sql"); err != nil {
			t.Fatalf("old insert: %v", err)
		}
	})
	t.Run("older", func(t *testing.T) {
		url := freshDatabase(t)
		unifiedMigrate(t, url, 300)
		for _, s := range []Sets{HubSet, ManagerSet} {
			d, err := Open(ctx, url, s)
			if err != nil {
				t.Fatalf("open %d: %v", s, err)
			}
			d.Close()
		}
		d, err := Open(ctx, url, AllSets)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		if tables := tablesOf(t, d); !slices.Equal(tables, all) {
			t.Fatalf("tables %v, want %v", tables, all)
		}
		var newer int
		if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE part IS NOT NULL AND version <= 300`).Scan(&newer); err != nil || newer != 0 {
			t.Fatalf("%d migrations up to 0300 ran again, %v", newer, err)
		}
	})
}

// The hub's settings are its own table: a database opened for the hub reads and writes hub_settings, one opened for a
// manager settings; neither sees the other's keys.
func TestSettingsAreEachSidesOwn(t *testing.T) {
	ctx := context.Background()
	url := freshDatabase(t)
	hub, err := Open(ctx, url, HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	manager, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := hub.PutSetting(ctx, "k", "hub's"); err != nil {
		t.Fatal(err)
	}
	if err := manager.PutSetting(ctx, "k", "manager's"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		d    *DB
		want string
	}{{hub, "hub's"}, {manager, "manager's"}} {
		var v string
		if ok, err := c.d.Setting(ctx, "k", &v); err != nil || !ok || v != c.want {
			t.Fatalf("got %q %v %v, want %q", v, ok, err, c.want)
		}
	}
	var v string
	if ok, err := hub.TakeSetting(ctx, "k", &v); err != nil || !ok || v != "hub's" {
		t.Fatalf("take %q %v %v", v, ok, err)
	}
	if ok, err := manager.Setting(ctx, "k", &v); err != nil || !ok || v != "manager's" {
		t.Fatalf("the hub's take reached the manager's: %q %v %v", v, ok, err)
	}
	if ok, err := manager.CompareSetting(ctx, "rev", 0, map[string]int{"revision": 1}); err != nil || !ok {
		t.Fatalf("compare: %v %v", ok, err)
	}
	if !slices.Contains(tablesOf(t, hub), "hub_settings") {
		t.Fatal("no hub_settings")
	}
	only := freshDatabase(t)
	d, err := Open(ctx, only, HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if slices.Contains(tablesOf(t, d), "settings") {
		t.Fatal("a hub's database has the managers' settings table")
	}
}

// On a database the hub and the default manager shared, the hub's keys are copied once into hub_settings (the
// manager's stay theirs), so nothing the hub kept is lost.
func TestHubSettingsCopiedOnUpgrade(t *testing.T) {
	ctx := context.Background()
	url := freshDatabase(t)
	unifiedMigrate(t, url, 419)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('hub_id', '"h-1"'), ('mutes', '{"spaces":["m/s"]}'),
		('codex_inventory/lap', '{"x":1}'), ('hub.warm_machines', '{}'), ('assistant.tasks', '[]')`); err != nil {
		t.Fatal(err)
	}
	hub, err := Open(ctx, url, HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	for _, k := range []string{"hub_id", "mutes", "codex_inventory/lap"} {
		var v any
		if ok, err := hub.Setting(ctx, k, &v); err != nil || !ok {
			t.Fatalf("%s not copied: %v %v", k, ok, err)
		}
	}
	for _, k := range []string{"hub.warm_machines", "assistant.tasks"} {
		var v any
		if ok, err := hub.Setting(ctx, k, &v); err != nil || ok {
			t.Fatalf("the manager's %s is the hub's now: %v %v", k, ok, err)
		}
	}
	manager, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	var tasks []any
	if ok, err := manager.Setting(ctx, "assistant.tasks", &tasks); err != nil || !ok {
		t.Fatalf("the manager's setting: %v %v", ok, err)
	}
}
