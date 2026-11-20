package db

import (
	"testing"
	"time"
)

func TestV2ReviewPrunedReceiptCannotBecomeFreshLedgerRow(t *testing.T) {
	d := open(t)
	ctx := t.Context()
	row := SentAsUser{Connector: "larkcontext", Action: "sent.record", MessageRef: "review-old", Text: "synthetic",
		At: time.Now().Add(-100 * 24 * time.Hour)}
	id, err := d.RecordSentAsUserOnce(ctx, row, false)
	if err != nil || id <= 0 {
		t.Fatalf("initial record ID %d: %v", id, err)
	}
	if n, err := d.PruneSentAsUser(ctx, time.Now().Add(-90*24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("pruned %d: %v", n, err)
	}
	row.At = time.Now()
	replayed, err := d.RecordSentAsUserOnce(ctx, row, false)
	if err == nil && replayed != 0 && replayed != id {
		t.Fatalf("replay after pruning minted a fresh row: original ID %d, new ID %d", id, replayed)
	}
}
