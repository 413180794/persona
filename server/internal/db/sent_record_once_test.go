package db

import (
	"errors"
	"sync"
	"testing"
)

func TestSentRecordOnceSurvivesLostResponse(t *testing.T) {
	d := open(t)
	row := SentAsUser{Connector: "larkcontext", Action: "sent.record", MessageRef: "message-42", Text: "hello"}
	if id, err := d.RecordSentAsUserOnce(t.Context(), row, true); err != nil || id != 0 {
		t.Fatalf("lookup of an unrecorded send: id=%d, %v", id, err)
	}
	first, err := d.RecordSentAsUserOnce(t.Context(), row, false)
	if err != nil || first == 0 {
		t.Fatalf("first ledger write: id=%d, %v", first, err)
	}
	var calls sync.WaitGroup
	for range 8 {
		calls.Go(func() {
			if id, err := d.RecordSentAsUserOnce(t.Context(), row, false); err != nil || id != first {
				t.Errorf("replayed ledger write: id=%d, %v", id, err)
			}
		})
	}
	calls.Wait()
	rows, _, err := d.SentAsUsers(t.Context(), 0, 10, row.Connector)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ledger after a lost reply and replay: %d rows, %v", len(rows), err)
	}
	row.Text = "different payload"
	if _, err := d.RecordSentAsUserOnce(t.Context(), row, false); !errors.Is(err, ErrSentRecordConflict) {
		t.Fatalf("a different payload with the same ref: %v", err)
	}
}

func TestSentRecordOnceRecoversOnlyUnambiguousLegacyEntries(t *testing.T) {
	d := open(t)
	row := SentAsUser{Connector: "larkcontext", Action: "sent.record", MessageRef: "legacy-42", Text: "hello"}
	first, err := d.RecordSentAsUser(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := d.RecordSentAsUserOnce(t.Context(), row, true); err != nil || id != first {
		t.Fatalf("lookup of legacy record: id=%d, %v", id, err)
	}
	if _, err := d.RecordSentAsUser(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordSentAsUserOnce(t.Context(), row, true); !errors.Is(err, ErrSentRecordConflict) {
		t.Fatalf("lookup of duplicate legacy records: %v", err)
	}
}
