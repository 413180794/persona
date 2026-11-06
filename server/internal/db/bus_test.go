package db

import (
	"context"
	"testing"
	"time"
)

func TestBusIdentities(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE bus_identities`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.pool.Exec(context.Background(), `TRUNCATE bus_identities`) })
	for _, b := range [][2]string{{"assistant", "h1"}, {"other", "h2"}} {
		if err := d.SaveBusIdentity(ctx, BusIdentity{ID: b[0], Role: "manager", Name: "名字 " + b[0]}, b[1]); err != nil {
			t.Fatal(err)
		}
	}
	b, err := d.BusIdentityByHash(ctx, "h2")
	if err != nil || b == nil || b.ID != "other" || b.Role != "manager" || b.Name != "名字 other" || b.CreatedAt.IsZero() {
		t.Fatalf("by hash: %+v %v", b, err)
	}
	if b, err := d.BusIdentityByHash(ctx, "nope"); err != nil || b != nil {
		t.Fatalf("unknown hash: %+v %v", b, err)
	}

	// A new token replaces the old one.
	if err := d.SaveBusIdentity(ctx, BusIdentity{ID: "assistant", Role: "manager", Name: "助手"}, "h1b"); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.BusIdentityByHash(ctx, "h1"); b != nil {
		t.Fatalf("the old token still works: %+v", b)
	}
	if b, _ := d.BusIdentityByHash(ctx, "h1b"); b == nil || b.Name != "助手" {
		t.Fatalf("the new token: %+v", b)
	}
	// Two identities cannot share a token.
	if err := d.SaveBusIdentity(ctx, BusIdentity{ID: "dup", Role: "manager"}, "h2"); err == nil {
		t.Fatal("a token hash taken twice")
	}

	if ok, err := d.DeleteBusIdentity(ctx, "assistant"); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	list, err := d.BusIdentities(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "other" {
		t.Fatalf("left %+v %v", list, err)
	}
	if ok, err := d.DeleteBusIdentity(ctx, "nobody"); ok || err != nil {
		t.Fatalf("deleting nobody: %v %v", ok, err)
	}
}

// failBusIdentityWrites makes every write of op (INSERT OR UPDATE, DELETE) on bus_identities fail until the returned
// func drops the trigger.
func failBusIdentityWrites(t *testing.T, d *DB, op string) func() {
	t.Helper()
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION test_fail_bus_identity() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'bus_identities write refused by the test'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(ctx, `CREATE TRIGGER test_fail_bus_identity BEFORE `+op+` ON bus_identities
		FOR EACH ROW EXECUTE FUNCTION test_fail_bus_identity()`); err != nil {
		t.Fatal(err)
	}
	drop := func() {
		_, _ = d.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_fail_bus_identity ON bus_identities`)
		_, _ = d.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS test_fail_bus_identity()`)
	}
	t.Cleanup(drop)
	return drop
}

func clearBusTables(t *testing.T, d *DB) {
	t.Helper()
	clear := func() {
		_, _ = d.pool.Exec(context.Background(), `TRUNCATE manager_install_codes, bus_identities`)
	}
	clear()
	t.Cleanup(clear)
}

// Redeeming a code uses it and saves the token together: when the save fails the code is still good, and once it
// worked the code is used up.
func TestManagerInstallCodeRedeemIsAtomic(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	clearBusTables(t, d)
	until := time.Now().Add(10 * time.Minute)
	if err := d.AddManagerInstallCode(ctx, "code1", ManagerInstallCode{ManagerID: "m-aaa111", Name: "工作", ExpiresAt: until}); err != nil {
		t.Fatal(err)
	}

	allow := failBusIdentityWrites(t, d, "INSERT OR UPDATE")
	if c, _, err := d.RedeemManagerInstallCode(ctx, "code1", "tok1"); err == nil {
		t.Fatalf("redeemed while the identity could not be saved: %+v", c)
	}
	allow()
	if b, _ := d.BusIdentityByID(ctx, "m-aaa111"); b != nil {
		t.Fatalf("an identity without a redeemed code: %+v", b)
	}

	c, name, err := d.RedeemManagerInstallCode(ctx, "code1", "tok1")
	if err != nil || c == nil || c.ManagerID != "m-aaa111" || name != "工作" {
		t.Fatalf("retry after a failed save: %+v %q %v", c, name, err)
	}
	if b, _ := d.BusIdentityByHash(ctx, "tok1"); b == nil || b.ID != "m-aaa111" || b.Role != "manager" || b.Name != "工作" {
		t.Fatalf("saved identity: %+v", b)
	}
	if c, _, err := d.RedeemManagerInstallCode(ctx, "code1", "tok2"); c != nil || err != nil {
		t.Fatalf("a code works once: %+v %v", c, err)
	}
	if c, _, err := d.RedeemManagerInstallCode(ctx, "nope", "tok3"); c != nil || err != nil {
		t.Fatalf("unknown code: %+v %v", c, err)
	}

	// A new code for a manager there is (moving it) gives it a new token and keeps the name it has now.
	if _, err := d.RenameBusIdentity(ctx, "m-aaa111", "家"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddManagerInstallCode(ctx, "code2", ManagerInstallCode{ManagerID: "m-aaa111", Name: "工作", ExpiresAt: until}); err != nil {
		t.Fatal(err)
	}
	if c, name, err := d.RedeemManagerInstallCode(ctx, "code2", "tok4"); err != nil || c == nil || name != "家" {
		t.Fatalf("moving: %+v %q %v", c, name, err)
	}
	if b, _ := d.BusIdentityByHash(ctx, "tok1"); b != nil {
		t.Fatalf("the old token still works: %+v", b)
	}
	if b, _ := d.BusIdentityByHash(ctx, "tok4"); b == nil || b.Name != "家" {
		t.Fatalf("the new token: %+v", b)
	}

	// A request that went away leaves the code good.
	if err := d.AddManagerInstallCode(ctx, "code3", ManagerInstallCode{ManagerID: "m-bbb222", Name: "新", ExpiresAt: until}); err != nil {
		t.Fatal(err)
	}
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if c, _, err := d.RedeemManagerInstallCode(gone, "code3", "tok5"); err == nil {
		t.Fatalf("redeemed with a cancelled context: %+v", c)
	}
	if c, _, err := d.RedeemManagerInstallCode(ctx, "code3", "tok5"); err != nil || c == nil {
		t.Fatalf("retry after a cancelled request: %+v %v", c, err)
	}
}

// Deleting an identity is all or nothing: when the identity cannot go, its codes stay too.
func TestDeleteBusIdentityIsAtomic(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	clearBusTables(t, d)
	if err := d.SaveBusIdentity(ctx, BusIdentity{ID: "m-ccc333", Role: "manager", Name: "x"}, "tokc"); err != nil {
		t.Fatal(err)
	}
	if err := d.AddManagerInstallCode(ctx, "codec", ManagerInstallCode{ManagerID: "m-ccc333", Name: "x", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	allow := failBusIdentityWrites(t, d, "DELETE")
	if ok, err := d.DeleteBusIdentity(ctx, "m-ccc333"); err == nil {
		t.Fatalf("deleted while the identity could not go: %v", ok)
	}
	allow()
	var codes int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM manager_install_codes WHERE manager_id = 'm-ccc333'`).Scan(&codes); err != nil || codes != 1 {
		t.Fatalf("codes left after a failed delete: %d %v", codes, err)
	}

	if ok, err := d.DeleteBusIdentity(ctx, "m-ccc333"); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM manager_install_codes WHERE manager_id = 'm-ccc333'`).Scan(&codes); err != nil || codes != 0 {
		t.Fatalf("codes left after the delete: %d %v", codes, err)
	}
}
