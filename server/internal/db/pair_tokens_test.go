package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPairTokenPersistenceAndAtomicRedemption(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(10 * time.Minute)
	if err := d.CreatePairToken(ctx, "pair-hash", expires, ""); err != nil {
		t.Fatal(err)
	}
	p, err := d.PairTokenByHash(ctx, "pair-hash")
	if err != nil || p == nil || p.CreatedAt.IsZero() || p.UsedAt != nil || p.DeviceID != "" || p.ExpiresAt.Sub(expires).Abs() > time.Microsecond {
		t.Fatalf("row %+v %v", p, err)
	}
	if p, err := d.PairTokenByHash(ctx, "unknown"); err != nil || p != nil {
		t.Fatalf("missing %+v %v", p, err)
	}
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			device := DeviceToken{ID: fmt.Sprintf("device-%d", i), Name: "小王的手机", Kind: "phone", Role: "full"}
			ok, err := d.RedeemPairToken(ctx, "pair-hash", device, fmt.Sprintf("device-hash-%d", i))
			results <- ok
			errors <- err
		}(i)
	}
	wg.Wait()
	close(results)
	close(errors)
	success := 0
	for ok := range results {
		if ok {
			success++
		}
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("redemptions %d", success)
	}
	devices, err := d.DeviceTokens(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices %+v %v", devices, err)
	}
	p, err = d.PairTokenByHash(ctx, "pair-hash")
	if err != nil || p.UsedAt == nil || p.DeviceID != devices[0].ID {
		t.Fatalf("used row %+v %v", p, err)
	}
}

func TestPairTokenExpiredAndFailedInsert(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	device := DeviceToken{ID: "device", Name: "小王的手机", Kind: "phone", Role: "full"}
	if err := d.CreatePairToken(ctx, "expired", time.Now().Add(-time.Second), ""); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"expired", "missing"} {
		if ok, err := d.RedeemPairToken(ctx, hash, device, "device-hash"); err != nil || ok {
			t.Fatalf("redeem %s = %v %v", hash, ok, err)
		}
	}
	if err := d.CreatePairToken(ctx, "unused", time.Now().Add(time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateDeviceToken(ctx, device, "existing-hash"); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.RedeemPairToken(ctx, "unused", device, "new-hash"); err == nil || ok {
		t.Fatal("duplicate device insert should roll back redemption")
	}
	p, err := d.PairTokenByHash(ctx, "unused")
	if err != nil || p.UsedAt != nil || p.DeviceID != "" {
		t.Fatalf("rollback row %+v %v", p, err)
	}
	device.ID = "another-device"
	if ok, err := d.RedeemPairToken(ctx, "unused", device, "new-hash"); err != nil || !ok {
		t.Fatalf("retry %v %v", ok, err)
	}
}

func TestRevokeDeviceTokenExpiresAssociatedPairToken(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(10 * time.Minute)
	for _, hash := range []string{"revoked-pair", "other-pair", "unused-pair"} {
		if err := d.CreatePairToken(ctx, hash, expires, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"revoked", "other"} {
		device := DeviceToken{ID: id, Name: id, Kind: "phone", Role: "full"}
		if ok, err := d.RedeemPairToken(ctx, id+"-pair", device, id+"-hash"); err != nil || !ok {
			t.Fatalf("redeem %s = %v %v", id, ok, err)
		}
	}
	if ok, err := d.RevokeDeviceToken(ctx, "revoked"); err != nil || !ok {
		t.Fatalf("revoke = %v %v", ok, err)
	}
	p, err := d.PairTokenByHash(ctx, "revoked-pair")
	if err != nil || p == nil || p.UsedAt == nil || p.DeviceID != "revoked" {
		t.Fatalf("revoked pair %+v %v", p, err)
	}
	if p.ExpiresAt.After(time.Now()) {
		t.Fatalf("revoked device pairing token still expires in the future: %v", p.ExpiresAt)
	}
	for _, hash := range []string{"other-pair", "unused-pair"} {
		p, err := d.PairTokenByHash(ctx, hash)
		if err != nil || p == nil || p.ExpiresAt.Sub(expires).Abs() > time.Microsecond {
			t.Fatalf("unrelated pair %s changed: %+v %v", hash, p, err)
		}
	}
}

// A pairing code made for one machine's persona-link carries the machine, and so does the credential it becomes.
func TestLinkPairTokenKeepsItsMachine(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if err := d.CreatePairToken(ctx, "link-pair", time.Now().Add(time.Minute), "laptop"); err != nil {
		t.Fatal(err)
	}
	p, err := d.PairTokenByHash(ctx, "link-pair")
	if err != nil || p == nil || p.MachineID != "laptop" {
		t.Fatalf("pair %+v %v", p, err)
	}
	device := DeviceToken{ID: "dev-link", Name: "笔记本 的 persona-link", Kind: "link", Role: "link", MachineID: "laptop"}
	if ok, err := d.RedeemPairToken(ctx, "link-pair", device, "link-hash"); !ok || err != nil {
		t.Fatalf("redeem %v %v", ok, err)
	}
	got, err := d.DeviceTokenByHash(ctx, "link-hash")
	if err != nil || got == nil || got.MachineID != "laptop" || got.Role != "link" {
		t.Fatalf("token %+v %v", got, err)
	}
	if err := d.CreateDeviceToken(ctx, DeviceToken{ID: "dev-phone2", Name: "手机", Kind: "phone", Role: "full"}, "phone-hash2"); err != nil {
		t.Fatal(err)
	}
	list, err := d.DeviceTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	machines := map[string]string{}
	for _, tok := range list {
		machines[tok.ID] = tok.MachineID
	}
	if machines["dev-link"] != "laptop" || machines["dev-phone2"] != "" {
		t.Fatalf("listed machines %v", machines)
	}
}
