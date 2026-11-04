package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
)

// A plugin's row round-trips; its secrets go with it; the audit keeps what happened.
func TestPluginRows(t *testing.T) {
	d, ctx := open(t), context.Background()
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	r := plugin.Record{ID: "example-notes", Source: plugin.SourceInstalled, Origin: "dir:/tmp/notes", Version: "1.2.0",
		Manifest: json.RawMessage(`{"id": "example-notes"}`), Granted: plugin.Permissions{Network: []string{"notes.example.com"}},
		GrantedDigest: "d1", Enabled: true, Config: json.RawMessage(`{"folders": ["a"]}`), InstalledAt: at, UpdatedAt: at}
	if err := d.PutPlugin(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Version, r.PreviousVersion, r.Remote, r.Enabled = "1.3.0", "1.2.0", &plugin.Remote{RPCSocket: "/tmp/r.sock"}, false
	if err := d.PutPlugin(ctx, r); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Plugins(ctx)
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
	if err := d.PutPluginSecret(ctx, "nope", "k", "s"); err == nil {
		t.Fatal("a secret of no plugin")
	}
	for _, v := range []string{"sealed-1", "sealed-2"} {
		if err := d.PutPluginSecret(ctx, "example-notes", "api_key", v); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.PutPluginSecret(ctx, "example-notes", "other", "sealed-3")
	_ = d.DeletePluginSecret(ctx, "example-notes", "other")
	if s, err := d.PluginSecrets(ctx, "example-notes"); err != nil || len(s) != 1 || s["api_key"] != "sealed-2" {
		t.Fatal(s, err)
	}
	if err := d.AddPluginAudit(ctx, "example-notes", "install", "cli", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.DeletePlugin(ctx, "example-notes"); err != nil {
		t.Fatal(err)
	}
	if s, _ := d.PluginSecrets(ctx, "example-notes"); len(s) != 0 {
		t.Fatal("its secrets went with it")
	}
	if rows, _ := d.Plugins(ctx); len(rows) != 0 {
		t.Fatal(rows)
	}
}
