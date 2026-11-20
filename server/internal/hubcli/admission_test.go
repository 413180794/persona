package hubcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/managerconf"
)

func TestAdmissionReconcileLedgerCLI(t *testing.T) {
	ctx := t.Context()
	shared := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if shared == "" {
		t.Skip("real isolated PostgreSQL required")
	}
	base, err := db.Open(ctx, shared, db.AllSets)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	ownName := db.DatabaseName(shared) + "_manager"
	ownURL, err := db.WithDatabase(shared, ownName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureDatabase(ctx, ownURL); err != nil {
		t.Fatal(err)
	}
	own, err := db.Open(ctx, ownURL, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()

	home := t.TempDir()
	t.Setenv("PERSONA_HOME", home)
	t.Setenv("PERSONA_DATABASE_URL", shared)
	if err := os.WriteFile(filepath.Join(home, "hub.json"), []byte(`{"token":"isolated-fixture-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "admission")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(admission.Env, dir)
	gate, err := admission.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		return runAdmission(ctx, args, &out)
	}
	if err := run("init"); err != nil {
		t.Fatal(err)
	}
	bindFixtureWriter(t, gate, own)
	operation, _, err := gate.AdmitLedger()
	if err != nil {
		t.Fatal(err)
	}
	row := db.SentAsUser{Connector: "cli-fixture", Action: "sent.record", Text: "synthetic", Operation: operation}
	if _, err := own.RecordSentAsUser(ctx, row); err != nil {
		t.Fatal(err)
	}
	id := operation.ID
	// A receipt in an unconfigured database is not enough to settle the ticket.
	const attempt = "cli-reconcile"
	if err := run("close", attempt); err != nil {
		t.Fatal(err)
	}
	if err := run("reconcile-ledger", attempt, id); err == nil {
		t.Fatal("shared database with no receipt settled the ticket")
	}
	if err := run("open", attempt); err == nil {
		t.Fatal("missing receipt unblocked the fence")
	}
	if err := managerconf.WriteOwnDatabase(home, managerconf.OwnDatabase{Name: ownName, Moved: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := run("reconcile-ledger", attempt, id); err == nil {
		t.Fatal("database with no Manager ownership settled the ticket")
	}
	if err := own.MarkMovedIn(ctx, "cli-test"); err != nil {
		t.Fatal(err)
	}
	if err := run("ledger-pending", attempt); err != nil {
		t.Fatal(err)
	}
	var pending admission.LedgerPending
	if err := json.Unmarshal(out.Bytes(), &pending); err != nil || len(pending.Operations) != 1 || pending.Operations[0] != id {
		t.Fatalf("pending output %q: %v", out.String(), err)
	}
	if err := run("reconcile-ledger", "wrong-attempt", id); err == nil {
		t.Fatal("another attempt settled the ticket")
	}
	for _, source := range []string{"database", "archive"} {
		if err := run("reconcile-ledger", attempt, id); err != nil {
			t.Fatal(err)
		}
		var receipt admission.LedgerSettlement
		if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || receipt.Status != "committed" || receipt.Operation != id || receipt.Authority != operation.Authority || receipt.ProofSource != source {
			t.Fatalf("reconcile output %q: %v", out.String(), err)
		}
	}
	if err := run("check", attempt); err != nil {
		t.Fatal(err)
	}
	if err := run("open", attempt); err != nil {
		t.Fatal(err)
	}

	// A stale shared database may still have an exact receipt after a move.
	bindFixtureWriter(t, gate, base)
	stale, _, err := gate.AdmitLedger()
	if err != nil {
		t.Fatal(err)
	}
	row.Operation = stale
	if _, err := base.RecordSentAsUser(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := base.MarkMovedOut(ctx, "cli-test"); err != nil {
		t.Fatal(err)
	}
	// Simulate an operator with a runtime directory lacking the moved database's configuration.
	if err := os.WriteFile(managerconf.DefaultFile(home), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const staleAttempt = "stale-shared"
	if err := run("close", staleAttempt); err != nil {
		t.Fatal(err)
	}
	if err := run("reconcile-ledger", staleAttempt, stale.ID); err == nil {
		t.Fatal("the exact receipt in a stale shared database settled the ticket")
	}
	if err := run("check", staleAttempt); err == nil {
		t.Fatal("the stale proof unblocked the fence")
	}

}

func bindFixtureWriter(t *testing.T, gate *admission.Gate, database *db.DB) admission.LedgerAuthority {
	t.Helper()
	authority, err := database.LedgerAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.BindLedgerWriter(authority); err != nil {
		t.Fatal(err)
	}
	return authority
}
