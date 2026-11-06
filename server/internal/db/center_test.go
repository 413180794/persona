package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMeshGroupAndConnectors(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE mesh_group, connector_credentials`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := d.MeshGroup(ctx, "hub-self"); err != nil || ok {
		t.Fatalf("empty group: ok=%v err=%v", ok, err)
	}
	if err := d.SaveMeshGroup(ctx, "hub-self", "hub-b", 1); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveMeshGroup(ctx, "hub-self", "hub-a", 2); err != nil {
		t.Fatal(err)
	}
	if c, term, ok, err := d.MeshGroup(ctx, "hub-self"); err != nil || !ok || c != "hub-a" || term != 2 {
		t.Fatalf("group %q %d %v %v", c, term, ok, err)
	}
	// Another hub on the same database (a restored backup, a shared test database) has no group from it.
	if _, _, ok, err := d.MeshGroup(ctx, "hub-other"); err != nil || ok {
		t.Fatalf("another hub's group leaked: ok=%v err=%v", ok, err)
	}
	if _, err := d.pool.Exec(ctx, `TRUNCATE mesh_group`); err != nil { // leave nothing for other tests on this database
		t.Fatal(err)
	}

	if c, err := d.ConnectorCredential(ctx, "example-workstation"); err != nil || c != nil {
		t.Fatalf("no credential yet: %+v %v", c, err)
	}
	if err := d.SaveConnectorCredential(ctx, ConnectorCredential{MachineID: "example-workstation", FP: "fp1", Token: "sealed1", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveConnectorCredential(ctx, ConnectorCredential{MachineID: "example-workstation", FP: "fp2", Token: "sealed2", Version: "v2"}); err != nil {
		t.Fatal(err)
	}
	if c, err := d.ConnectorCredential(ctx, "example-workstation"); err != nil || c == nil || c.FP != "fp2" || c.Token != "sealed2" || c.Version != "v2" {
		t.Fatalf("replaced credential %+v %v", c, err)
	}
	if ok, err := d.DeleteConnectorCredential(ctx, "example-workstation"); err != nil || !ok {
		t.Fatalf("delete %v %v", ok, err)
	}
	if ok, _ := d.DeleteConnectorCredential(ctx, "example-workstation"); ok {
		t.Fatal("deleted twice")
	}
}

func TestAppTokensMoveToTheCenter(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id LIKE 'dev_ct_%'`); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		t    DeviceToken
		hash string
	}{
		{DeviceToken{ID: "dev_ct_phone", Name: "iPhone", Kind: "phone", Role: "full"}, "ct-hash-phone"},
		{DeviceToken{ID: "dev_ct_watch", Name: "Watch", Kind: "watch", Role: "answer"}, "ct-hash-watch"},
		{DeviceToken{ID: "dev_ct_link", Name: "link", Kind: "link", Role: "link", MachineID: "mac"}, "ct-hash-link"},
		{DeviceToken{ID: "dev_ct_gone", Name: "old", Kind: "phone", Role: "full"}, "ct-hash-gone"},
	} {
		if err := d.CreateDeviceToken(ctx, x.t, x.hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.RevokeDeviceToken(ctx, "dev_ct_gone"); err != nil {
		t.Fatal(err)
	}
	list, err := d.AppTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []ExportedToken
	for _, e := range list {
		if len(e.ID) > 7 && e.ID[:7] == "dev_ct_" {
			mine = append(mine, e)
		}
	}
	if len(mine) != 2 || mine[0].Hash == "" {
		t.Fatalf("exported %+v (want the phone and the watch, with hashes)", mine)
	}
	// Taken over elsewhere: here, the same rows are already there (nothing new); under new ids, they are added.
	if n, err := d.ImportDeviceTokens(ctx, mine); err != nil || n != 0 {
		t.Fatalf("import of known ones: %d %v", n, err)
	}
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id IN ('dev_ct_phone', 'dev_ct_watch')`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.ImportDeviceTokens(ctx, mine); err != nil || n != 2 {
		t.Fatalf("import: %d %v", n, err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "ct-hash-watch"); tok == nil || tok.Role != "answer" || tok.ID != "dev_ct_watch" {
		t.Fatalf("imported %+v", tok)
	}
}

// A credential revoked on another hub dies here too, and one this hub never saw stays revoked when a copy arrives later.
func TestRevokedTokensFollowToOtherHubs(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id LIKE 'dev_rv_%'`); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		t    DeviceToken
		hash string
	}{
		{DeviceToken{ID: "dev_rv_phone", Name: "iPhone", Kind: "phone", Role: "full"}, "rv-hash-phone"},
		{DeviceToken{ID: "dev_rv_done", Name: "old", Kind: "phone", Role: "full"}, "rv-hash-done"},
		{DeviceToken{ID: "dev_rv_link", Name: "link", Kind: "link", Role: "link", MachineID: "mac"}, "rv-hash-link"},
	} {
		if err := d.CreateDeviceToken(ctx, x.t, x.hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.RevokeDeviceToken(ctx, "dev_rv_done"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertDevice(ctx, Device{DeviceID: "rv-push", Kind: "phone", CredentialID: "dev_rv_phone"}); err != nil {
		t.Fatal(err)
	}
	unknown := ExportedToken{DeviceToken: DeviceToken{ID: "dev_rv_unknown", Name: "elsewhere", Kind: "phone", Role: "full"}, Hash: "rv-hash-unknown"}
	ids, err := d.ApplyRevokedTokens(ctx, []ExportedToken{
		{DeviceToken: DeviceToken{ID: "dev_rv_phone", Role: "full"}, Hash: "rv-hash-phone"},
		{DeviceToken: DeviceToken{ID: "dev_rv_done", Role: "full"}, Hash: "rv-hash-done"},
		{DeviceToken: DeviceToken{ID: "dev_rv_link", Role: "full"}, Hash: "rv-hash-link"},  // the role here is what counts
		{DeviceToken: DeviceToken{ID: "dev_rv_done", Role: "full"}, Hash: "rv-hash-other"}, // a known id under another hash
		unknown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "dev_rv_phone" {
		t.Fatalf("newly revoked: %v (want only the phone)", ids)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "rv-hash-phone"); tok != nil {
		t.Fatalf("the phone's credential still works: %+v", tok)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "rv-hash-link"); tok == nil {
		t.Fatal("a persona-link credential was revoked")
	}
	var devices int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE credential_id = 'dev_rv_phone'`).Scan(&devices); err != nil || devices != 0 {
		t.Fatalf("the phone's push registrations: %d %v", devices, err)
	}
	if n, err := d.ImportDeviceTokens(ctx, []ExportedToken{unknown}); err != nil || n != 0 {
		t.Fatalf("a copy of a revoked credential handed over: %d %v", n, err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "rv-hash-unknown"); tok != nil {
		t.Fatalf("a revoked credential came back: %+v", tok)
	}
	shown, err := d.DeviceTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range shown {
		if x.ID == "dev_rv_unknown" {
			t.Fatalf("a credential this hub only knows as revoked elsewhere is in the device list: %+v", x)
		}
	}
	list, err := d.RevokedAppTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range list {
		if strings.HasPrefix(e.ID, "dev_rv_") {
			got[e.ID] = e.Hash
		}
	}
	if len(got) != 3 || got["dev_rv_phone"] != "rv-hash-phone" || got["dev_rv_done"] != "rv-hash-done" || got["dev_rv_unknown"] != "rv-hash-unknown" {
		t.Fatalf("revoked app credentials: %v", got)
	}
}

// A revocation that arrives while the credential's import is still open waits for it and revokes it.
func TestRevocationDuringImportWins(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id = 'dev_rv_race'`); err != nil {
		t.Fatal(err)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash) VALUES ('dev_rv_race', 'p', 'phone', 'full', 'rv-hash-race')`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ids []string
		err error
	}
	done := make(chan result, 1)
	go func() {
		ids, err := d.ApplyRevokedTokens(ctx, []ExportedToken{{DeviceToken: DeviceToken{ID: "dev_rv_race", Name: "p", Kind: "phone", Role: "full"}, Hash: "rv-hash-race"}})
		done <- result{ids, err}
	}()
	time.Sleep(200 * time.Millisecond) // it is waiting on the open insert by now
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "rv-hash-race"); tok != nil {
		t.Fatalf("the credential came through its revocation: %+v (newly revoked: %v)", tok, r.ids)
	}
}
