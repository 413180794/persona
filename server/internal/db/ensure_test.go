package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/db/dbtest"
)

// A manager's database is made in the same server the first time and found the next; it is a database of its own,
// migrated apart from the one it was derived from.
func TestEnsureDatabase(t *testing.T) {
	base := os.Getenv(dbtest.Env)
	if base == "" {
		t.Skip(dbtest.Env + " not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "persona_mgr_t_" + hex.EncodeToString(b)
	url, err := WithDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin, err := pgx.Connect(context.Background(), base)
		if err == nil {
			_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			_ = admin.Close(context.Background())
		}
	})
	if DatabaseName(url) != name || DatabaseName(base) == name {
		t.Fatalf("%s from %s", url, base)
	}
	if created, err := EnsureDatabase(ctx, url); err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	if created, err := EnsureDatabase(ctx, url); err != nil || created {
		t.Fatalf("second: %v %v", created, err)
	}
	d, err := Open(ctx, url, ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.PutSetting(ctx, "mgr.test", "theirs"); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, base, AllSets)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var v string
	if ok, err := other.Setting(ctx, "mgr.test", &v); err != nil || ok {
		t.Fatalf("the setting is in the other database too: %v %v %q", ok, err, v)
	}
	if _, err := EnsureDatabase(ctx, "postgres://u@[fd00::20]:5432/Bad-Name"); err == nil {
		t.Fatal("a name that is not plain was accepted")
	}
	if _, err := WithDatabase("mysql://x/y", "z"); err == nil {
		t.Fatal("not postgres")
	}
}
