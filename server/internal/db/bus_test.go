package db

import (
	"context"
	"testing"
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
