package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// CopyReport is what CopyTables copied: rows by table.
type CopyReport struct {
	Rows map[string]int64
}

// ErrNotEmpty: the database to copy into already holds the manager's data: it is never copied over.
var ErrNotEmpty = errors.New("its own database already has data")

// ErrSameDatabase: the two are one database.
var ErrSameDatabase = errors.New("the two are one database")

// CopyTables copies the manager set's tables from from into to: the move of the default manager's data out of the
// database it shared with the hub into its own (docs/server/server.md "The default manager's own database"). Both are open
// and migrated (Open) by this build, so their tables are the same. to must be another database (ErrSameDatabase) with
// none of the manager's data (ErrNotEmpty: a copy never goes over one, nor over one made by an earlier copy). One
// transaction on to, which also records the copy there (manager_moved 'in', MovedIn): the data and that record go
// together. From one snapshot of from (its tables as they were together, only read). Tables in foreign-key order, as
// COPY text with pg_dump's formats, row counts checked against the snapshot, sequences as from had them.
func CopyTables(ctx context.Context, from, to *DB, logf func(string, ...any)) (CopyReport, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	rep := CopyReport{Rows: map[string]int64{}}
	fromID, err := from.Identity(ctx)
	if err != nil {
		return rep, err
	}
	toID, err := to.Identity(ctx)
	if err != nil {
		return rep, err
	}
	if fromID == toID {
		return rep, ErrSameDatabase
	}
	order, err := to.copyOrder(ctx)
	if err != nil {
		return rep, err
	}
	mine := ManagerTables()
	var tables []string
	for _, t := range order {
		// Each database's own records: of the copy, and of the key its secrets are sealed with (sealed.go; the
		// copied ones are sealed with the hub's, which the key rotation re-seals after the move).
		if slices.Contains(mine, t) && t != "manager_moved" && t != "sealing_key" {
			tables = append(tables, t)
		}
	}
	src, err := from.pool.Acquire(ctx)
	if err != nil {
		return rep, err
	}
	defer src.Release()
	srcTx, err := src.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return rep, err
	}
	defer srcTx.Rollback(context.WithoutCancel(ctx))
	if err := dumpFormats(ctx, srcTx); err != nil {
		return rep, err
	}
	have := map[string]bool{}
	rows, err := srcTx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p')`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return rep, err
		}
		have[t] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}
	err = pgx.BeginFunc(ctx, to.pool, func(tx pgx.Tx) error {
		if err := dumpFormats(ctx, tx); err != nil {
			return err
		}
		// Nothing of the manager's there yet (a fresh database: no migration of the manager's set adds rows), and no
		// earlier copy: one is never copied over.
		var moved bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM manager_moved WHERE direction = 'in')`).Scan(&moved); err != nil {
			return err
		}
		if moved {
			return ErrNotEmpty
		}
		for _, t := range tables {
			var any bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+pgx.Identifier{t}.Sanitize()+`)`).Scan(&any); err != nil {
				return err
			}
			if any {
				return fmt.Errorf("%w (%s)", ErrNotEmpty, t)
			}
		}
		for _, t := range tables {
			if !have[t] {
				continue // newer than what from had (none, since both are migrated: kept for a table only to has)
			}
			cols, err := copyColumns(ctx, tx, t)
			if err != nil {
				return err
			}
			if len(cols) == 0 {
				continue
			}
			n, err := copyTable(ctx, srcTx, tx, t, cols, logf)
			if err != nil {
				return fmt.Errorf("%s: %w", t, err)
			}
			// Every row there before it counts as copied: the same count in the source's snapshot and here.
			var want, got int64
			if err := srcTx.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{t}.Sanitize()).Scan(&want); err != nil {
				return fmt.Errorf("%s: count: %w", t, err)
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{t}.Sanitize()).Scan(&got); err != nil {
				return fmt.Errorf("%s: count: %w", t, err)
			}
			if got != want || n != want {
				return fmt.Errorf("%s: %d rows copied of %d (%d there)", t, n, want, got)
			}
			rep.Rows[t] = n
			if err := copySequences(ctx, srcTx, tx, t, cols); err != nil {
				return fmt.Errorf("%s: sequences: %w", t, err)
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO manager_moved (direction, peer) VALUES ('in', $1)`, fromID)
		return err
	})
	return rep, err
}

// dumpFormats sets tx's text formats to pg_dump's, so COPY text means the same on both sides whatever each server's
// defaults are (DateStyle above all).
func dumpFormats(ctx context.Context, tx pgx.Tx) error {
	for _, q := range []string{`SET LOCAL DateStyle = 'ISO, YMD'`, `SET LOCAL IntervalStyle = 'postgres'`,
		`SET LOCAL extra_float_digits = 3`, `SET LOCAL TimeZone = 'UTC'`} {
		if _, err := tx.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// Identity names d's database for comparing: its server's system identifier and the database's name (no credentials).
func (d *DB) Identity(ctx context.Context) (string, error) {
	var sys int64
	var name string
	if err := d.pool.QueryRow(ctx, `SELECT system_identifier, current_database() FROM pg_control_system()`).Scan(&sys, &name); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/%s", sys, name), nil
}

// MovedIn: d is the default manager's own database and the copy that filled it went through (CopyTables), at.
func (d *DB) MovedIn(ctx context.Context) (at time.Time, ok bool, err error) {
	return d.moved(ctx, "in")
}

// MovedOut: the default manager's data left d, the database it shared with the hub, at (MarkMovedOut).
func (d *DB) MovedOut(ctx context.Context) (at time.Time, ok bool, err error) {
	return d.moved(ctx, "out")
}

func (d *DB) moved(ctx context.Context, direction string) (time.Time, bool, error) {
	var at time.Time
	err := d.pool.QueryRow(ctx, `SELECT at FROM manager_moved WHERE direction = $1`, direction).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return at, err == nil, err
}

// MarkMovedIn records in d, the default manager's own database, that it is its data's (a fresh install: nothing to
// copy).
func (d *DB) MarkMovedIn(ctx context.Context, peer string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO manager_moved (direction, peer) VALUES ('in', $1) ON CONFLICT (direction) DO NOTHING`, peer)
	return err
}

// MarkMovedOut records in d, the database the default manager shared with the hub, that its data left for peer.
func (d *DB) MarkMovedOut(ctx context.Context, peer string) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO manager_moved (direction, peer) VALUES ('out', $1) ON CONFLICT (direction) DO NOTHING`, peer)
	return err
}

// copyOrder is d's tables but schema_migrations (its own), parents before the tables that refer to them.
func (d *DB) copyOrder(ctx context.Context) ([]string, error) {
	rows, err := d.pool.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p') AND NOT c.relispartition
		AND c.relname <> 'schema_migrations' ORDER BY c.relname`)
	if err != nil {
		return nil, err
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	rows, err = d.pool.Query(ctx, `SELECT c.relname, p.relname FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid JOIN pg_class p ON p.oid = k.confrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE k.contype = 'f' AND n.nspname = current_schema() AND c.oid <> p.oid`)
	if err != nil {
		return nil, err
	}
	parents := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			rows.Close()
			return nil, err
		}
		parents[child] = append(parents[child], parent)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []string
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(t string) error
	visit = func(t string) error {
		switch state[t] {
		case 1:
			return errors.New("foreign keys in a cycle at " + t)
		case 2:
			return nil
		}
		state[t] = 1
		for _, p := range parents[t] {
			if slices.Contains(tables, p) {
				if err := visit(p); err != nil {
					return err
				}
			}
		}
		state[t] = 2
		out = append(out, t)
		return nil
	}
	for _, t := range tables {
		if err := visit(t); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// copyColumns are table's columns that take values (not generated, not dropped), in order.
func copyColumns(ctx context.Context, q pgx.Tx, table string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT a.attname FROM pg_attribute a WHERE a.attrelid = $1::regclass AND a.attnum > 0
		AND NOT a.attisdropped AND a.attgenerated = '' ORDER BY a.attnum`, pgx.Identifier{table}.Sanitize())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// copyProgressEvery is how often a table that takes long says how far its copy got.
var copyProgressEvery = 10 * time.Second

// counter counts the bytes read through it.
type counter struct {
	r io.Reader
	n atomic.Int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// copyTable streams table's cols from src to dst as COPY text, saying how far it got while it takes long.
func copyTable(ctx context.Context, src, dst pgx.Tx, table string, cols []string, logf func(string, ...any)) (int64, error) {
	list := make([]string, len(cols))
	for i, c := range cols {
		list[i] = pgx.Identifier{c}.Sanitize()
	}
	names := pgx.Identifier{table}.Sanitize() + " (" + strings.Join(list, ", ") + ")"
	pr, pw := io.Pipe()
	read := make(chan error, 1)
	go func() {
		_, err := src.Conn().PgConn().CopyTo(ctx, pw, `COPY (SELECT `+strings.Join(list, ", ")+` FROM `+pgx.Identifier{table}.Sanitize()+`) TO STDOUT`)
		pw.CloseWithError(err)
		read <- err
	}()
	in := &counter{r: pr}
	done := make(chan struct{})
	go func() {
		start := time.Now()
		t := time.NewTicker(copyProgressEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				logf("database: copying %s: %d MB so far (%s)", table, in.n.Load()>>20, time.Since(start).Round(time.Second))
			}
		}
	}()
	tag, err := dst.Conn().PgConn().CopyFrom(ctx, in, `COPY `+names+` FROM STDIN`)
	close(done)
	pr.CloseWithError(err)
	if rerr := <-read; err == nil {
		err = rerr
	}
	return tag.RowsAffected(), err
}

// copySequences sets each of table's serial columns' sequence in dst as src's is (never below what the column holds):
// ids handed out before the move are not handed out again.
func copySequences(ctx context.Context, src, dst pgx.Tx, table string, cols []string) error {
	for _, c := range cols {
		var seq *string
		if err := dst.QueryRow(ctx, `SELECT pg_get_serial_sequence($1, $2)`, pgx.Identifier{table}.Sanitize(), c).Scan(&seq); err != nil {
			return err
		}
		if seq == nil {
			continue
		}
		var srcSeq *string
		if err := src.QueryRow(ctx, `SELECT pg_get_serial_sequence($1, $2)`, pgx.Identifier{table}.Sanitize(), c).Scan(&srcSeq); err != nil {
			return err
		}
		last, called := int64(0), false
		if srcSeq != nil {
			if err := src.QueryRow(ctx, `SELECT last_value, is_called FROM `+*srcSeq).Scan(&last, &called); err != nil {
				return err
			}
		}
		var top *int64
		col, tab := pgx.Identifier{c}.Sanitize(), pgx.Identifier{table}.Sanitize()
		if err := dst.QueryRow(ctx, `SELECT max(`+col+`)::bigint FROM `+tab).Scan(&top); err != nil {
			return err
		}
		if top != nil && *top >= last {
			last, called = *top, true
		}
		if last < 1 {
			last, called = 1, false
		}
		if _, err := dst.Exec(ctx, `SELECT setval($1, $2, $3)`, *seq, last, called); err != nil {
			return err
		}
	}
	return nil
}

// CopySize is how much of from CopyTables would copy into to: the tables' size on disk, with their indexes.
func CopySize(ctx context.Context, from, to *DB) (int64, error) {
	tables, err := to.copyOrder(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	err = from.pool.QueryRow(ctx, `SELECT coalesce(sum(pg_total_relation_size(c.oid)), 0)::bigint FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = current_schema() AND c.relname = ANY($1)`, tables).Scan(&n)
	return n, err
}

// DataDirectory is where d's server keeps its files, when its role may read it (a superuser, pg_read_all_settings).
func (d *DB) DataDirectory(ctx context.Context) (string, bool) {
	var dir string
	if err := d.pool.QueryRow(ctx, `SHOW data_directory`).Scan(&dir); err != nil || dir == "" {
		return "", false
	}
	return dir, true
}

// HubTables are the tables the hub's set makes (its migrations' CREATE TABLE, and RENAME TO): what a database the hub
// shared with the default manager keeps of its own once the manager's tables are dropped there.
func HubTables() []string { return tablesOfSet("hub") }

// ManagerTables are the tables the manager's set makes: what CopyTables copies.
func ManagerTables() []string { return tablesOfSet("manager") }

func tablesOfSet(set string) []string {
	files, err := fs.Glob(migrations, "migrations/"+set+"/*.sql")
	if err != nil {
		return nil
	}
	create := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)`)
	rename := regexp.MustCompile(`(?i)RENAME\s+TO\s+"?([a-z_][a-z0-9_]*)`)
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		b, err := fs.ReadFile(migrations, f)
		if err != nil {
			continue
		}
		for _, re := range []*regexp.Regexp{create, rename} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if t := strings.ToLower(m[1]); !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// Tables are d's tables but schema_migrations, sorted.
func (d *DB) Tables(ctx context.Context) ([]string, error) {
	tables, err := d.copyOrder(ctx)
	if err != nil {
		return nil, err
	}
	slices.Sort(tables)
	return tables, nil
}

// DropTables drops tables from d, in one transaction (with what depends on them).
func (d *DB) DropTables(ctx context.Context, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	quoted := make([]string, len(tables))
	for i, t := range tables {
		quoted[i] = pgx.Identifier{t}.Sanitize()
	}
	_, err := d.pool.Exec(ctx, `DROP TABLE IF EXISTS `+strings.Join(quoted, ", ")+` CASCADE`)
	return err
}
