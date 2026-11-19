package db

import (
	"context"
	"errors"
	"testing"
)

func TestNodeSessionReceiptCommitAndStaleReplay(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	const machine, id = "node-state-fixture", "session-fixture"
	first := NodeSession{Launcher: "claude", Pane: "first"}
	if err := d.PutNodeSessionReceipt(ctx, machine, id, "", "op-first", first); err != nil {
		t.Fatal(err)
	}
	if err := d.PutNodeSessionReceipt(ctx, machine, id, "", "op-first", first); err != nil {
		t.Fatal("duplicate lost-reply retry failed:", err)
	}
	second := first
	second.Pane = "second"
	if err := d.PutNodeSessionReceipt(ctx, machine, id, "op-first", "op-second", second); err != nil {
		t.Fatal(err)
	}
	if err := d.PutNodeSessionReceipt(ctx, machine, id, "", "op-first", first); !errors.Is(err, ErrNodeSessionRevision) {
		t.Fatalf("stale replay returned %v", err)
	}
	rows, err := d.NodeSessions(ctx, machine)
	if err != nil || rows[id].Pane != "second" {
		t.Fatalf("state after replay = %+v, err %v", rows[id], err)
	}
	current, err := d.NodeSessionReceipt(ctx, machine, id)
	if err != nil || current != "op-second" {
		t.Fatalf("commit receipt = %q, err %v", current, err)
	}
	if err := d.PutNodeSessionReceipt(ctx, machine, id, "op-second", "op-invalid", NodeSession{Pane: "\x00"}); err == nil {
		t.Fatal("invalid state unexpectedly wrote")
	}
	current, err = d.NodeSessionReceipt(ctx, machine, id)
	if err != nil || current != "op-second" {
		t.Fatalf("failed transaction changed the receipt: %q, %v", current, err)
	}
}
