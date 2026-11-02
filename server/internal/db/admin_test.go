package db

import (
	"context"
	"testing"
	"time"
)

func TestAdminCredentialsAndSessions(t *testing.T) {
	d, ctx := open(t), context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE admin_credentials, admin_sessions`); err != nil {
		t.Fatal(err)
	}
	if h, err := d.AdminPasswordHash(ctx); err != nil || h != "" {
		t.Fatalf("unset: %q %v", h, err)
	}
	// First-run setup: only the first of two racing browsers sets it.
	if ok, err := d.SetAdminPassword(ctx, "h1", true); err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	if ok, err := d.SetAdminPassword(ctx, "h2", true); err != nil || ok {
		t.Fatalf("second: %v %v", ok, err)
	}
	if h, _ := d.AdminPasswordHash(ctx); h != "h1" {
		t.Fatalf("hash %q", h)
	}
	now := time.Now()
	for _, s := range []AdminSession{
		{IDHash: "a", CSRF: "ca", ExpiresAt: now.Add(time.Hour), Source: "192.0.2.0/24", UserAgent: "x"},
		{IDHash: "b", CSRF: "cb", ExpiresAt: now.Add(time.Hour)},
		{IDHash: "old", CSRF: "co", ExpiresAt: now.Add(-time.Minute)},
	} {
		if err := d.CreateAdminSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if s, err := d.AdminSessionByHash(ctx, "a"); err != nil || s == nil || s.CSRF != "ca" || s.Source != "192.0.2.0/24" {
		t.Fatalf("a: %+v %v", s, err)
	}
	if s, _ := d.AdminSessionByHash(ctx, "old"); s != nil {
		t.Fatal("expired session found")
	}
	// A password change keeps only the browser that made it.
	if err := d.SetAdminPasswordSignOut(ctx, "h3", "a"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.AdminSessionByHash(ctx, "b"); s != nil {
		t.Fatal("b survived the password change")
	}
	if s, _ := d.AdminSessionByHash(ctx, "a"); s == nil {
		t.Fatal("a was signed out")
	}
	if err := d.DeleteAdminSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.AdminSessionByHash(ctx, "a"); s != nil {
		t.Fatal("a after logout")
	}
	_ = d.CreateAdminSession(ctx, AdminSession{IDHash: "c", CSRF: "cc", ExpiresAt: now.Add(time.Hour)})
	if err := d.ClearAdminPassword(ctx); err != nil {
		t.Fatal(err)
	}
	if h, _ := d.AdminPasswordHash(ctx); h != "" {
		t.Fatal("password survived clear")
	}
	if s, _ := d.AdminSessionByHash(ctx, "c"); s != nil {
		t.Fatal("session survived clear")
	}
	if err := d.DeleteAdminSessions(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
