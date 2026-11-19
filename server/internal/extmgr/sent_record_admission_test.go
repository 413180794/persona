package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
)

func TestSentRecordAdmissionTracksAcceptedTransactions(t *testing.T) {
	st := storagePostgres(t)
	for _, method := range []string{connector.MethodSentRecord, connector.MethodSentRecordOnce} {
		t.Run(method, func(t *testing.T) {
			ctx := t.Context()
			const attempt = "ledger-release"
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			gate, err := admission.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gate.Control("init", ""); err != nil {
				t.Fatal(err)
			}
			h := newTestManager(t)
			h.store, h.opts.Admission = st, gate
			const cid = "releaseledger"
			spec := sentExtensionSpec(t, h, "release-ledger", `[{"id":"releaseledger","name":"Release Ledger","capabilities":["status"],"sends_as_user":true}]`)
			params := fmt.Sprintf(`{"contribution":%q,"target":{"chat":"fixture"},"text":"synthetic","message_ref":%q}`, cid, method)
			lookup := json.RawMessage(strings.TrimSuffix(params, "}") + `,"lookup_only":true}`)
			accepted := make(chan error, 1)
			client, disconnect := context.WithCancel(ctx)
			defer disconnect()
			if err := st.Transaction(ctx, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "LOCK TABLE sent_as_user IN SHARE MODE"); err != nil {
					return err
				}
				go func() {
					// Discard the response, as if the extension disconnected after sending the frame.
					_, err := spec.Handle(client, method, json.RawMessage(params))
					accepted <- err
				}()
				const trackedKind = "extension-ledger-record"
				deadline := time.Now().Add(4 * time.Second)
				for {
					gateState, err := gate.Control("status", "")
					if err != nil {
						return err
					}
					var waiting bool
					if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks
						WHERE locktype = 'relation' AND relation = 'sent_as_user'::regclass
						AND mode = 'RowExclusiveLock' AND NOT granted)`).Scan(&waiting); err != nil {
						return err
					}
					if gateState.Active[trackedKind] == 1 && waiting {
						break
					}
					if time.Now().After(deadline) {
						return fmt.Errorf("cannot observe an admitted transaction waiting on the ledger: %+v waiting=%v", gateState, waiting)
					}
					time.Sleep(10 * time.Millisecond)
				}
				if _, err := gate.Control("close", attempt); err != nil {
					return err
				}
				disconnect()
				state, err := gate.Control("check", attempt)
				if err != nil || state.Phase != "draining" || state.Active[trackedKind] != 1 {
					return fmt.Errorf("in-flight transaction missing from the gate: %+v %v", state, err)
				}
				for _, denied := range []string{connector.MethodSentRecord, connector.MethodSentRecordOnce} {
					_, err := spec.Handle(ctx, denied, json.RawMessage(params))
					wantRPCCode(t, "new write on closed gate "+denied, err, "admission_closed")
				}
				pending, err := gate.LedgerPending(attempt)
				if err != nil || len(pending.Operations) != 1 {
					return fmt.Errorf("missing in-flight operation: %+v %v", pending, err)
				}
				if _, err := gate.ReconcileLedger(ctx, attempt, pending.Operations[0], st.SentRecordOperationCommitted); err == nil {
					return errors.New("an in-flight operation was settled")
				}
				result, err := spec.Handle(ctx, connector.MethodSentRecordOnce, lookup)
				if err != nil || result != (connector.SentRecordOnceResult{MessageRef: method, Status: connector.SentRecordMissing}) {
					return fmt.Errorf("lookup while the transaction is pending: %+v %v", result, err)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-accepted:
				if err != nil {
					t.Fatalf("admitted transaction did not finish: %v", err)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("admitted transaction did not finish")
			}
			state, err := gate.Control("check", attempt)
			if err != nil || state.Phase != "drained" || len(state.Active) != 0 {
				t.Fatalf("committed transaction did not drain: %+v %v", state, err)
			}
			result, err := spec.Handle(ctx, connector.MethodSentRecordOnce, lookup)
			if err != nil || result != (connector.SentRecordOnceResult{MessageRef: method, Status: connector.SentRecordRecorded}) {
				t.Fatalf("closed-gate lookup did not recover lost response: %+v %v", result, err)
			}
			if _, err := gate.Control("open", attempt); err != nil {
				t.Fatal(err)
			}
			if method == connector.MethodSentRecordOnce {
				// A new extension retries the exact write. The older writer's
				// unknown reply was handled solely by the lookup above.
				if _, err := spec.Handle(ctx, method, json.RawMessage(params)); err != nil {
					t.Fatal(err)
				}
			}
			var rows, guards int
			if err := st.QueryRow(ctx, `SELECT count(*) FROM sent_as_user WHERE connector=$1 AND message_ref=$2`, cid, method).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if err := st.QueryRow(ctx, `SELECT count(*) FROM sent_record_guards`).Scan(&guards); err != nil {
				t.Fatal(err)
			}
			if rows != 1 {
				t.Fatalf("expected one committed row, got %d", rows)
			}
			// The previous RPC tests may already have written guards on this database.
			var ownGuard bool
			if err := st.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sent_record_guards
				WHERE key = sha256(convert_to($1, 'UTF8') || decode('00', 'hex') ||
				convert_to('sent.record', 'UTF8') || decode('00', 'hex') || convert_to($2, 'UTF8')))`, cid, method).Scan(&ownGuard); err != nil {
				t.Fatal(err)
			}
			if ownGuard != (method == connector.MethodSentRecordOnce) {
				t.Fatalf("lookup-only or legacy mutated the guard: own=%v (total %d)", ownGuard, guards)
			}
		})
	}
}

type ambiguousSentStore struct{ *db.DB }

func (s ambiguousSentStore) RecordSentAsUser(ctx context.Context, row db.SentAsUser) (int64, error) {
	if _, err := s.DB.RecordSentAsUser(ctx, row); err != nil {
		return 0, err
	}
	return 0, errors.New("fixture lost the database response after commit")
}

func (s ambiguousSentStore) RecordSentAsUserOnce(ctx context.Context, row db.SentAsUser, lookup bool) (int64, error) {
	id, err := s.DB.RecordSentAsUserOnce(ctx, row, lookup)
	if err != nil || lookup {
		return id, err
	}
	return 0, errors.New("fixture lost the database response after commit")
}

func TestSentRecordAdmissionKeepsUnsettledWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Control("init", ""); err != nil {
		t.Fatal(err)
	}
	st := storagePostgres(t)
	h := newTestManager(t)
	h.store, h.opts.Admission = ambiguousSentStore{st}, gate
	spec := sentExtensionSpec(t, h, "unknown-ledger", `[{"id":"unknownledger","name":"Unknown Ledger","capabilities":["status"],"sends_as_user":true}]`)
	if err := sentRecord(spec, `{"contribution":"unknownledger","target":{},"text":"","message_ref":"ambiguous-db-reply"}`); err == nil {
		t.Fatal("fixture did not lose the response")
	}
	const attempt = "ledger-unknown"
	if _, err := gate.Control("close", attempt); err != nil {
		t.Fatal(err)
	}
	state, err := gate.Control("check", attempt)
	if err != nil || state.Phase != "draining" || state.Active["extension-ledger-record"] != 1 {
		t.Fatalf("an unsettled database reply permitted the gate to drain: %+v %v", state, err)
	}
}

// This extends the release review's commit/lookup probe through reconciliation
// and reopening. A mode without a reference must have equally precise proof.
func TestSentRecordAdmissionReconcilesOnlyTheCommittedOperation(t *testing.T) {
	st := storagePostgres(t)
	for _, row := range []struct {
		name, method, messageRef string
	}{
		{"legacy_reference", connector.MethodSentRecord, "proved-commit"},
		{"legacy_no_reference", connector.MethodSentRecord, ""},
		{"guarded_reuses_an_existing_row", connector.MethodSentRecordGuarded, "guarded-commit"},
	} {
		t.Run(row.name, func(t *testing.T) {
			ctx := t.Context()
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			gate, err := admission.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gate.Control("init", ""); err != nil {
				t.Fatal(err)
			}
			h := newTestManager(t)
			h.store, h.opts.Admission = st, gate
			spec := sentExtensionSpec(t, h, "release-receipt", `[{"id":"receiptledger","name":"Receipts","capabilities":["status"],"sends_as_user":true}]`)
			params := fmt.Sprintf(`{"contribution":"receiptledger","target":{},"text":"private-fixture-body","message_ref":%q}`, row.messageRef)
			if row.method == connector.MethodSentRecordGuarded {
				if _, err := spec.Handle(ctx, row.method, json.RawMessage(params)); err != nil {
					t.Fatal(err)
				}
			}
			h.store = ambiguousSentStore{st}
			if _, err := spec.Handle(ctx, row.method, json.RawMessage(params)); err == nil {
				t.Fatal("fixture did not lose the reply")
			}
			// This ticket has no proof despite other committed ledger rows.
			unproved, finishUnproved, err := gate.AdmitLedger()
			if err != nil {
				t.Fatal(err)
			}
			const attempt = "ledger-reconcile"
			if _, err := gate.Control("close", attempt); err != nil {
				t.Fatal(err)
			}
			if row.messageRef != "" {
				lookup := json.RawMessage(strings.TrimSuffix(params, "}") + `,"lookup_only":true}`)
				result, err := spec.Handle(ctx, connector.MethodSentRecordOnce, lookup)
				if result != (connector.SentRecordOnceResult{MessageRef: row.messageRef, Status: connector.SentRecordRecorded}) || err != nil {
					t.Fatalf("ledger lookup: %#v %v", result, err)
				}
			}
			afterRestart, err := admission.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := afterRestart.LedgerPending(attempt)
			if err != nil || len(pending.Operations) != 2 || pending.Unidentified != 0 {
				t.Fatalf("expected two identifiable operations: %+v %v", pending, err)
			}
			committed := pending.Operations[0]
			if committed == unproved {
				committed = pending.Operations[1]
			}
			if _, err := afterRestart.ReconcileLedger(ctx, "wrong-attempt", committed, st.SentRecordOperationCommitted); err == nil {
				t.Fatal("wrong attempt was allowed to reconcile")
			}
			if _, err := afterRestart.ReconcileLedger(ctx, attempt, unproved, st.SentRecordOperationCommitted); err == nil {
				t.Fatal("another operation's receipt was accepted")
			}
			if n, err := st.PruneSentAsUser(ctx, time.Now().Add(time.Minute)); err != nil || n < 1 {
				t.Fatalf("fixture prune: %d %v", n, err)
			}
			settled, err := afterRestart.ReconcileLedger(ctx, attempt, committed, st.SentRecordOperationCommitted)
			if err != nil || settled.Operation != committed || settled.Attempt != attempt || settled.Status != "committed" {
				t.Fatalf("cannot reconcile the committed operation: %+v %v", settled, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, settled.Audit))
			if err != nil || string(data) != committed+"\n" {
				t.Fatalf("unexpected audit file: %q %v", data, err)
			}
			repeat, err := afterRestart.ReconcileLedger(ctx, attempt, committed, func(context.Context, string) (bool, error) {
				t.Fatal("repeated reconciliation should read the durable audit")
				return false, nil
			})
			if err != nil || repeat != settled {
				t.Fatalf("failed to replay reconciliation: %+v %v", repeat, err)
			}
			if _, err := afterRestart.Control("open", attempt); err == nil {
				t.Fatal("reconciling one ticket unlocked another")
			}
			finishUnproved()
			drained, err := afterRestart.Control("check", attempt)
			if err != nil || drained.Phase != "drained" || len(drained.Active) != 0 {
				t.Fatalf("settled fence failed to drain: %+v %v", drained, err)
			}
			if _, err := afterRestart.Control("open", attempt); err != nil {
				t.Fatal(err)
			}
			if _, err := afterRestart.ReconcileLedger(ctx, attempt, committed, st.SentRecordOperationCommitted); err == nil {
				t.Fatal("reconcile succeeded on the open fence")
			}
		})
	}
}
