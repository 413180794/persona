// Package dbtest gives a test binary a Postgres database of its own (#583). Every run of the server's tests used to
// point at the one JARVIS_TEST_DATABASE_URL database (jarvis_test): several lanes and a deploy testing on the hub
// machine at once truncated and migrated each other's tables mid-test, and tests failed with empty or foreign rows.
//
// A package with database tests calls Main from its TestMain: it creates jarvis_test_<time>_<random> next to the
// configured database, points JARVIS_TEST_DATABASE_URL at it for the tests (db.Open migrates it), and drops it
// afterwards. A role that may not create databases falls back to a lock: the runs then take turns on the shared one.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Env names the database the tests use; without it they skip what needs one.
const Env = "JARVIS_TEST_DATABASE_URL"

// stale is how old a leftover database (a run that was killed) has to be before a later run drops it.
const stale = 6 * time.Hour

var ownName = regexp.MustCompile(`^jarvis_test_(\d{10})_[0-9a-f]{8}$`)

// ownedBy says whether u is already a run's own database (set by a parent test binary).
func ownedBy(u string) bool {
	pu, err := url.Parse(u)
	return err == nil && ownName.MatchString(strings.TrimPrefix(pu.Path, "/"))
}

// Main runs m on a database of its own and returns the exit code (os.Exit(dbtest.Main(m))).
func Main(m *testing.M) int {
	base := identity.Getenv(Env)
	if base != "" {
		_ = os.Setenv(Env, base)
		_ = os.Setenv("PERSONA_TEST_DATABASE_URL", base)
	}
	if base == "" || ownedBy(base) {
		// A test binary a test started again (a fake node, a plugin) runs on its parent's database, which the parent
		// drops: its child is often killed before it could.
		return m.Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	own, done, err := Own(ctx, base)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: %v\n", err)
		return 1
	}
	defer done()
	_ = os.Setenv(Env, own)
	_ = os.Setenv("PERSONA_TEST_DATABASE_URL", own)
	return m.Run()
}

// Own makes a database for one run from base (a URL of a database the role can connect to) and returns its URL and
// how to drop it. When the role may not create databases, it is base itself, held by a lock until done.
func Own(ctx context.Context, base string) (string, func(), error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", nil, fmt.Errorf("parse %s: %w", Env, err)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", nil, fmt.Errorf("connect: %w", err)
	}
	sweep(ctx, admin)
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := fmt.Sprintf("jarvis_test_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
	_, err = admin.Exec(ctx, `CREATE DATABASE "`+name+`"`)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42501" { // insufficient_privilege: take turns on the shared database
		if _, err := admin.Exec(ctx, `SELECT pg_advisory_lock(hashtext('jarvis_test'))`); err != nil {
			_ = admin.Close(ctx)
			return "", nil, fmt.Errorf("lock: %w", err)
		}
		return base, func() { _ = admin.Close(context.Background()) }, nil // closing the session releases the lock
	}
	if err != nil {
		_ = admin.Close(ctx)
		return "", nil, fmt.Errorf("create database: %w", err)
	}
	own := *u
	own.Path = "/" + name
	return own.String(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		drop(ctx, admin, name)
		_ = admin.Close(ctx)
	}, nil
}

// drop removes a run's database, closing what is still connected to it (WITH FORCE, Postgres 13 and later).
func drop(ctx context.Context, admin *pgx.Conn, name string) {
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+name+`"`)
	}
}

// sweep drops the databases runs that were killed left behind, once they are old enough not to be in use.
func sweep(ctx context.Context, admin *pgx.Conn) {
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'jarvis\_test\_%'`)
	if err != nil {
		return
	}
	var old []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		m := ownName.FindStringSubmatch(name)
		if m == nil {
			continue // not one of ours (jarvis_test_deploy…)
		}
		if at, err := strconv.ParseInt(m[1], 10, 64); err == nil && time.Since(time.Unix(at, 0)) > stale {
			old = append(old, name)
		}
	}
	rows.Close()
	for _, name := range old {
		drop(ctx, admin, name)
	}
}
