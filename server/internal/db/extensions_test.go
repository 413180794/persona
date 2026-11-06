package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
)

// A plugin's row round-trips; its secrets go with it; the audit keeps what happened.
func TestExtensionRows(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	r := extension.Record{ID: "example-notes", Source: extension.SourceInstalled, Origin: "dir:/tmp/notes", Version: "1.2.0",
		Manifest: json.RawMessage(`{"id": "example-notes"}`), Granted: extension.Permissions{Network: []string{"notes.example.com"}},
		GrantedDigest: "d1", Enabled: true, Config: json.RawMessage(`{"folders": ["a"]}`), InstalledAt: at, UpdatedAt: at}
	if err := d.PutExtension(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Version, r.PreviousVersion, r.Remote, r.Enabled = "1.3.0", "1.2.0", &extension.Remote{RPCSocket: "/tmp/r.sock"}, false
	if err := d.PutExtension(ctx, r); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Extensions(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	got := rows[0]
	if got.Version != "1.3.0" || got.PreviousVersion != "1.2.0" || got.Enabled || got.Remote == nil || got.Remote.RPCSocket != "/tmp/r.sock" ||
		got.Granted.Network[0] != "notes.example.com" || !got.InstalledAt.Equal(at) {
		t.Fatalf("%+v", got)
	}
	var cfg map[string][]string
	if json.Unmarshal(got.Config, &cfg) != nil || cfg["folders"][0] != "a" {
		t.Fatal(string(got.Config))
	}
	if err := d.PutExtensionSecret(ctx, "nope", "k", "s"); err == nil {
		t.Fatal("a secret of no plugin")
	}
	for _, v := range []string{"sealed-1", "sealed-2"} {
		if err := d.PutExtensionSecret(ctx, "example-notes", "api_key", v); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.PutExtensionSecret(ctx, "example-notes", "other", "sealed-3")
	_ = d.DeleteExtensionSecret(ctx, "example-notes", "other")
	if s, err := d.ExtensionSecrets(ctx, "example-notes"); err != nil || len(s) != 1 || s["api_key"] != "sealed-2" {
		t.Fatal(s, err)
	}
	if err := d.AddExtensionAudit(ctx, "example-notes", "install", "cli", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteExtension(ctx, "example-notes"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.ExtensionSecrets(ctx, "example-notes"); len(s) != 0 {
		t.Fatal("its secrets went with it")
	}
	if rows, _ := d.Extensions(ctx); len(rows) != 0 {
		t.Fatal(rows)
	}
}

// A market's row round-trips, its index and token with it.
func TestExtensionMarketRows(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	r := extension.MarketRecord{ID: "m1", Spec: "github:example/plugins", Name: "Example", TokenSealed: "sealed", AddedAt: at}
	if err := d.PutExtensionMarket(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Index, r.Warnings, r.Error, r.FetchedAt = json.RawMessage(`{"market_version": 1}`), []string{"w"}, "", &at
	if err := d.PutExtensionMarket(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := d.PutExtensionMarket(ctx, extension.MarketRecord{ID: "m2", Spec: "github:example/plugins", AddedAt: at}); err == nil {
		t.Fatal("the same market twice")
	}
	rows, err := d.ExtensionMarkets(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if g := rows[0]; g.TokenSealed != "sealed" || !strings.Contains(string(g.Index), "market_version") || g.Warnings[0] != "w" || g.FetchedAt == nil || !g.AddedAt.Equal(at) {
		t.Fatalf("%+v", g)
	}
	if err := d.DeleteExtensionMarket(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := d.ExtensionMarkets(ctx); len(rows) != 0 {
		t.Fatal(rows)
	}
}

// The audit reads back newest first, by extension (or connector, or the bot) or everyone's, at most limit rows, with
// its details as they were written.
func TestExtensionAudits(t *testing.T) {
	d, ctx := open(t), context.Background()
	ids := []string{"example-audit-a", "example-audit-b"}
	clean := func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM plugin_audit WHERE plugin = ANY($1)`, ids); err != nil {
			t.Fatal(err)
		}
	}
	clean()
	t.Cleanup(clean)
	for _, r := range []struct{ id, action, actor, detail string }{
		{ids[0], "install", "personad extension", `{"version":"1.0.0"}`},
		{ids[1], "connector_signed_in", "device:dev_phone", `{"account":"Example"}`},
		{ids[0], "connector_logout", "hub token", ``},
	} {
		if err := d.AddExtensionAudit(ctx, r.id, r.action, r.actor, json.RawMessage(r.detail)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ExtensionAudits(ctx, ids[0], 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != "connector_logout" || got[1].Action != "install" || got[1].Actor != "personad extension" ||
		string(got[0].Detail) != "{}" || got[1].At.After(got[0].At) {
		t.Fatalf("by extension: %+v", got)
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(got[1].Detail, &v); err != nil || v.Version != "1.0.0" {
		t.Fatalf("detail %s (%v)", got[1].Detail, err)
	}
	all, err := d.ExtensionAudits(ctx, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Plugin != ids[0] || all[0].Action != "connector_logout" || all[1].Plugin != ids[1] {
		t.Fatalf("everyone's, at most 2: %+v", all)
	}
	if none, err := d.ExtensionAudits(ctx, "example-audit-none", 10); err != nil || len(none) != 0 {
		t.Fatalf("an extension without rows: %+v (%v)", none, err)
	}
}
