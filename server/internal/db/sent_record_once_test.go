package db

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
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
	if n, err := d.PruneSentAsUser(t.Context(), time.Now().Add(time.Minute)); err != nil || n != 2 {
		t.Fatalf("pruned legacy entries: %d rows, %v", n, err)
	}
	if id, err := d.RecordSentAsUserOnce(t.Context(), row, true); id != 0 || err != nil {
		t.Fatalf("pruned legacy lookup without a guard should remain missing: id=%d %v", id, err)
	}
}

func TestSentRecordMixedVersionsAndShortenedRetention(t *testing.T) {
	d := open(t)
	ctx := t.Context()
	recorded := time.Now().UTC().Add(-48 * time.Hour)
	for n := range 8 {
		row := SentAsUser{Connector: "larkcontext", Action: "sent.record",
			MessageRef: fmt.Sprintf("mixed-%d", n), Text: "synthetic", At: recorded}
		var legacyID, exactID int64
		start := make(chan struct{})
		var callers sync.WaitGroup
		callers.Go(func() {
			<-start
			var err error
			legacyID, err = d.RecordSentAsUser(ctx, row)
			if err != nil {
				t.Error(err)
			}
		})
		callers.Go(func() {
			<-start
			var err error
			exactID, err = d.RecordSentAsUserOnce(ctx, row, false)
			if err != nil {
				t.Error(err)
			}
		})
		close(start)
		callers.Wait()
		if legacyID == 0 || legacyID != exactID {
			t.Fatalf("mixed protocol writes created different rows: legacy %d, exact %d", legacyID, exactID)
		}
	}
	if n, err := d.PruneSentAsUser(ctx, recorded.Add(-88*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("90-day retention: pruned %d, %v", n, err)
	}
	if n, err := d.PruneSentAsUser(ctx, recorded.Add(24*time.Hour)); err != nil || n != 8 {
		t.Fatalf("shortened to one day: pruned %d, %v", n, err)
	}
	// A different pool observes the guard regardless of process or request state.
	restarted, err := Open(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"), ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	row := SentAsUser{Connector: "larkcontext", Action: "sent.record", MessageRef: "mixed-0", Text: "synthetic"}
	for _, lookup := range []bool{true, false} {
		if id, err := restarted.RecordSentAsUserOnce(ctx, row, lookup); id != 0 || !errors.Is(err, ErrSentRecordPruned) {
			t.Fatalf("after shortened retention, lookup=%v: id=%d %v", lookup, id, err)
		}
	}
	if id, err := restarted.RecordSentAsUser(ctx, row); id != 0 || !errors.Is(err, ErrSentRecordPruned) {
		t.Fatalf("legacy cannot append a guarded ref after pruning: id=%d %v", id, err)
	}
	rows, _, err := restarted.SentAsUsers(ctx, 0, 10, row.Connector)
	if err != nil || len(rows) != 0 {
		t.Fatalf("a replay recreated a pruned row: %d rows, %v", len(rows), err)
	}
}
