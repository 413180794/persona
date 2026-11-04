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
