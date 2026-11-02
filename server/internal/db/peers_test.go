package db

import (
	"context"
	"testing"
	"time"
)

func TestPeersAndPeerCodes(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE peers, peer_codes`); err != nil {
		t.Fatal(err)
	}
	if err := d.CreatePeerCode(ctx, "h1", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.PeerCodeValid(ctx, "h1"); err != nil || !ok {
		t.Fatalf("fresh code valid = %v, %v", ok, err)
	}
	p := Peer{HubID: "hub-b", Name: "B", FP: "fp-b", Endpoints: []string{"https://hub.example.com:18662"}, Token: "sealed", CredentialID: "dev_b"}
	cred := DeviceToken{ID: "dev_b", Name: "B", Kind: "peer", Role: "peer", MachineID: "hub-b"}
	if ok, err := d.RedeemPeerCode(ctx, "h1", p, cred, "credhash1"); err != nil || !ok {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	// One use only, and nothing changes the second time.
	cred2 := DeviceToken{ID: "dev_b2", Name: "B", Kind: "peer", Role: "peer", MachineID: "hub-b"}
	if ok, err := d.RedeemPeerCode(ctx, "h1", p, cred2, "credhash2"); err != nil || ok {
		t.Fatalf("second redeem = %v, %v", ok, err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "credhash2"); tok != nil {
		t.Fatal("a used code still made a credential")
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "credhash1"); tok == nil || tok.Role != "peer" || tok.MachineID != "hub-b" {
		t.Fatalf("credential %+v", tok)
	}
	list, err := d.Peers(ctx)
	if err != nil || len(list) != 1 || list[0].FP != "fp-b" || list[0].Endpoints[0] != "https://hub.example.com:18662" {
		t.Fatalf("peers %+v, %v", list, err)
	}
	// Pairing again with a new code revokes the credential issued before.
	if err := d.CreatePeerCode(ctx, "h2", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.RedeemPeerCode(ctx, "h2", p, cred2, "credhash2"); err != nil || !ok {
		t.Fatalf("re-pair = %v, %v", ok, err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "credhash1"); tok != nil {
		t.Fatal("the old peer credential still works")
	}
	if err := d.UpdatePeerSeen(ctx, "hub-b", "B2", []string{"https://[2001:db8::1]:18662"}); err != nil {
		t.Fatal(err)
	}
	list, _ = d.Peers(ctx)
	if list[0].Name != "B2" || list[0].Endpoints[0] != "https://[2001:db8::1]:18662" {
		t.Fatalf("after seen: %+v", list[0])
	}
	// An expired code is not valid.
	if err := d.CreatePeerCode(ctx, "h3", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.PeerCodeValid(ctx, "h3"); ok {
		t.Fatal("expired code valid")
	}
	if ok, err := d.DeletePeer(ctx, "hub-b"); err != nil || !ok {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if ok, _ := d.DeletePeer(ctx, "hub-b"); ok {
		t.Fatal("deleted twice")
	}
	p.HubID = "hub-c"
	if err := d.SavePeer(ctx, p); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.Peers(ctx); len(list) != 1 || list[0].HubID != "hub-c" {
		t.Fatalf("saved %+v", list)
	}
}
