package db

import (
	"errors"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
)

func TestLedgerAuthorityChecksSystemIdentifierAndOID(t *testing.T) {
	d := open(t)
	ctx := t.Context()
	authority, err := d.LedgerAuthority(ctx)
	if err != nil {
		t.Fatal(err)
	}
	operation := admission.LedgerOperation{ID: "10101010101010101010101010101010", Authority: authority}
	row := SentAsUser{Connector: "authority", Action: "sent.record", Operation: operation}
	if _, err := d.RecordSentAsUser(ctx, row); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.SentRecordOperationCommitted(ctx, operation); err != nil || !ok {
		t.Fatalf("original proof: %v %v", ok, err)
	}
	for _, wrong := range []admission.LedgerAuthority{
		{SystemIdentifier: "1", DatabaseOID: authority.DatabaseOID},
		{SystemIdentifier: authority.SystemIdentifier, DatabaseOID: authority.DatabaseOID + 1},
	} {
		operation.Authority = wrong
		if ok, err := d.SentRecordOperationCommitted(ctx, operation); ok || !errors.Is(err, ErrLedgerAuthorityMismatch) {
			t.Fatalf("wrong authority accepted a copied receipt: %v %v", ok, err)
		}
		row.Operation = admission.LedgerOperation{ID: "20202020202020202020202020202020", Authority: wrong}
		if _, err := d.RecordSentAsUser(ctx, row); !errors.Is(err, ErrLedgerAuthorityMismatch) {
			t.Fatalf("wrong authority allowed a write: %v", err)
		}
	}
	rows, _, err := d.SentAsUsers(ctx, 0, 10, row.Connector)
	if err != nil || len(rows) != 1 {
		t.Fatalf("wrong target was mutated: %d %v", len(rows), err)
	}
}
