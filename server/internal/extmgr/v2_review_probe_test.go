package extmgr

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/connector"
)

func TestV2ReviewClosedGateDoesNotAdmitNewLedgerRows(t *testing.T) {
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
	if _, err := gate.Control("close", "review-attempt"); err != nil {
		t.Fatal(err)
	}
	if state, err := gate.Control("check", "review-attempt"); err != nil || state.Phase != "drained" {
		t.Fatalf("cannot establish a closed test gate: %+v %v", state, err)
	}
	store := storagePostgres(t)
	h := newTestManager(t)
	h.store, h.opts.Admission = store, gate
	spec := sentExtensionSpec(t, h, "v2-review", `[{"id":"reviewledger","name":"Review Ledger","capabilities":["status"],"sends_as_user":true}]`)
	params := json.RawMessage(`{"contribution":"reviewledger","target":{"chat":"fixture"},"text":"synthetic","message_ref":"v2-review-ref"}`)
	_, _ = spec.Handle(t.Context(), connector.MethodSentRecordOnce, params)
	rows, _, err := store.SentAsUsers(t.Context(), 0, 10, "reviewledger")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("closed, drained admission gate admitted %d new ledger rows", len(rows))
	}
}

func TestV2ReviewLegacyIgnoresUnknownOnceField(t *testing.T) {
	store := storagePostgres(t)
	h := newTestManager(t)
	h.store = store
	spec := sentExtensionSpec(t, h, "v2-review", `[{"id":"reviewledger","name":"Review Ledger","capabilities":["status"],"sends_as_user":true}]`)
	params := json.RawMessage(`{"contribution":"reviewledger","target":{"chat":"fixture"},"text":"synthetic","message_ref":"v2-review-ref","lookup_only":"ignored legacy field"}`)
	if _, err := spec.Handle(t.Context(), connector.MethodSentRecord, params); err != nil {
		t.Fatalf("legacy caller's formerly ignored field changed the result: %v", err)
	}
}
