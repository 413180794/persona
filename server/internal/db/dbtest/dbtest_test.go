package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Two runs at once get two databases, each usable and gone when its run ends; a database a killed run left behind
// long ago is swept by the next run, one of another kind is left alone.
func TestOwnDatabases(t *testing.T) {
	base := os.Getenv(Env)
	if base == "" {
		t.Skip(Env + " not set")
	}
	ctx := context.Background()
	a, doneA, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	b, doneB, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == base {
		t.Fatalf("not databases of their own: %s, %s", a, b)
	}
	for _, u := range []string{a, b} {
		c, err := pgx.Connect(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Exec(ctx, `CREATE TABLE t (x int); INSERT INTO t VALUES (1)`); err != nil {
			t.Fatal(err)
		}
		_ = c.Close(ctx)
	}
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	exists := func(u string) bool {
		pu, _ := url.Parse(u)
		var n int
		_ = admin.QueryRow(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, strings.TrimPrefix(pu.Path, "/")).Scan(&n)
		return n == 1
	}
	doneA()
	if exists(a) || !exists(b) {
		t.Fatalf("after the first run: a %v, b %v", exists(a), exists(b))
	}
	doneB()
	if exists(b) {
		t.Fatal("the second database is still there")
	}

	// Names of this run's own: two runs of this test at once must not create the same database.
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	tag := hex.EncodeToString(buf)
	old := "persona_test_" + itoa(time.Now().Add(-7*time.Hour).Unix()) + "_" + tag
	other := "persona_test_sweep_" + tag
	for _, n := range []string{old, other} {
		_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS "`+n+`"`)
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+n+`"`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+other+`"`) })
	_, done, err := Own(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if exists("/"+old) || !exists("/"+other) {
		t.Fatalf("sweep: old %v, other %v", exists("/"+old), exists("/"+other))
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// A test binary started again by a test (a fake node) inherits its parent's own database and makes none of its own.
func TestOwnedBy(t *testing.T) {
	for u, want := range map[string]bool{
		"postgres://u:p@127.0.0.1:5432/persona_test_1793642187_12cf738f?sslmode=disable": true,
		"postgres://u:p@127.0.0.1:5432/persona_test?sslmode=disable":                     false,
		"postgres://u:p@127.0.0.1:5432/persona_test_deploy?sslmode=disable":              false,
	} {
		if got := ownedBy(u); got != want {
			t.Errorf("%s: %v", u, got)
		}
	}
}
