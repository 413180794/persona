package hubcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/admission"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/managerconf"
)

func TestReleaseReviewDoesNotSettleFromWrongOwnedPrimaryClone(t *testing.T) {
	ctx := t.Context()
	shared := os.Getenv("PERSONA_TEST_DATABASE_URL")
	if shared == "" {
		t.Skip("isolated PostgreSQL required")
	}
	admin, err := pgx.Connect(ctx, shared)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	authoritative, staged := "fan_fixture_authority", "fan_fixture_staged"
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{authoritative}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	ownURL, err := db.WithDatabase(shared, authoritative)
	if err != nil {
		t.Fatal(err)
	}
	own, err := db.Open(ctx, ownURL, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	if err := own.MarkMovedIn(ctx, "synthetic-owner"); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("PERSONA_HOME", home)
	t.Setenv("PERSONA_DATABASE_URL", shared)
	if err := os.WriteFile(filepath.Join(home, "hub.json"), []byte(`{"token":"isolated-review-fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := managerconf.WriteOwnDatabase(home, managerconf.OwnDatabase{Name: authoritative, Moved: time.Now()}); err != nil {
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
	out := new(bytes.Buffer)
	run := func(args ...string) error {
		out.Reset()
		return runAdmission(ctx, args, out)
	}
	if err := run("init"); err != nil {
		t.Fatal(err)
	}
	authority := bindFixtureWriter(t, gate, own)
	operation, _, err := gate.AdmitLedger()
	if err != nil {
		t.Fatal(err)
	}
	row := db.SentAsUser{Connector: "isolated-fixture", Action: "sent.record", Text: "synthetic", Operation: operation}
	if _, err := own.RecordSentAsUser(ctx, row); err != nil {
		t.Fatal(err)
	}
	// A second committed ticket tests that archive readback cannot settle other work.
	other, _, err := gate.AdmitLedger()
	if err != nil {
		t.Fatal(err)
	}
	row.Operation = other
	if _, err := own.RecordSentAsUser(ctx, row); err != nil {
		t.Fatal(err)
	}
	own.Close()

	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{staged}.Sanitize()+" WITH TEMPLATE "+pgx.Identifier{authoritative}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	query := "SELECT oid FROM pg_database WHERE datname=$1"
	var ownOID, copyOID uint32
	if err := admin.QueryRow(ctx, query, authoritative).Scan(&ownOID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, query, staged).Scan(&copyOID); err != nil {
		t.Fatal(err)
	}
	if ownOID == copyOID {
		t.Fatal("fixture copy must have a distinct OID")
	}
	setRoute := func(name string) {
		t.Helper()
		if err := managerconf.WriteOwnDatabase(home, managerconf.OwnDatabase{Name: name, Moved: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// The source and candidate share receipts, ownership, and system ID. The
	// candidate route is already configured before the operator closes the fence.
	setRoute(staged)
	const attempt = "original-fixture-route"
	if err := run("close", attempt); err != nil {
		t.Fatal(err)
	}
	var closed admission.State
	if err := json.Unmarshal(out.Bytes(), &closed); err != nil || closed.LedgerAuthority == nil ||
		*closed.LedgerAuthority != authority || closed.LedgerAuthority.DatabaseOID != ownOID {
		t.Fatalf("close did not pin the running writer: %s (%v)", out.String(), err)
	}
	clone, err := db.Attach(ctx, mustDatabaseURL(t, shared, staged), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	cloneIdentity, err := clone.LedgerAuthority(ctx)
	if err != nil || cloneIdentity.SystemIdentifier != authority.SystemIdentifier || cloneIdentity.DatabaseOID != copyOID {
		t.Fatalf("unexpected identity for cloned database: %+v %v", cloneIdentity, err)
	}
	copyOperation := operation
	copyOperation.Authority = cloneIdentity
	if ok, err := clone.SentRecordOperationCommitted(ctx, copyOperation); err != nil || !ok {
		t.Fatalf("fixture clone has no matching receipt: %v %v", ok, err)
	}
	if _, owned, err := clone.MovedIn(ctx); err != nil || !owned {
		t.Fatalf("fixture clone has no copied ownership: %v %v", owned, err)
	}
	assertRejected := func(op string) {
		t.Helper()
		if err := run("reconcile-ledger", attempt, op); !errors.Is(err, db.ErrLedgerAuthorityMismatch) {
			t.Fatalf("wrong primary OID %d (original %d) accepted the receipt: %s %v", copyOID, ownOID, out.String(), err)
		}
		if err := run("check", attempt); err == nil {
			t.Fatal("cloned receipt unblocked the original fence")
		}
	}
	assertRejected(operation.ID)
	clone.Close()

	// Put the clone behind the exact original database name as well. A name
	// match cannot stand in for the OID read from the connected database.
	setRoute(authoritative)
	rename := func(from, to string) {
		t.Helper()
		if _, err := admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{from}.Sanitize()+" RENAME TO "+pgx.Identifier{to}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	const held = "fan_fixture_held"
	rename(authoritative, held)
	rename(staged, authoritative)
	assertRejected(operation.ID)
	rename(authoritative, staged)
	rename(held, authoritative)
	if err := run("close", attempt); err != nil {
		t.Fatal(err)
	}
	// A lost CLI response after the rename must preserve one historical proof.
	args := []string{"reconcile-ledger", attempt, operation.ID}
	if err := runAdmission(ctx, args, lostReadback{}); !errors.Is(err, errLostReadback) {
		t.Fatalf("expected interruption after archiving, got %v", err)
	}
	setRoute(staged)
	if err := run(args...); err != nil {
		t.Fatal(err)
	}
	var archived admission.LedgerSettlement
	if err := json.Unmarshal(out.Bytes(), &archived); err != nil || archived.ProofSource != "archive" ||
		archived.Operation != operation.ID || archived.Authority != authority {
		t.Fatalf("wrong route was represented as new proof: %s (%v)", out.String(), err)
	}
	assertRejected(other.ID)
	setRoute(authoritative)
	if err := run("reconcile-ledger", attempt, other.ID); err != nil {
		t.Fatal(err)
	}
	var fresh admission.LedgerSettlement
	if err := json.Unmarshal(out.Bytes(), &fresh); err != nil || fresh.ProofSource != "database" || fresh.Authority != authority {
		t.Fatalf("restored primary did not supply fresh proof: %s (%v)", out.String(), err)
	}
	if err := run("check", attempt); err != nil {
		t.Fatal(err)
	}
	if err := run("open", attempt); err != nil {
		t.Fatal(err)
	}
	t.Logf("binding retained original OID %d, refused clone OID %d on config change and same-name replacement, survived interrupted readback, then reopened only on the original", ownOID, copyOID)
}

var errLostReadback = errors.New("fixture output disconnected")

type lostReadback struct{}

func (lostReadback) Write([]byte) (int, error) { return 0, errLostReadback }

func mustDatabaseURL(t *testing.T, shared, name string) string {
	t.Helper()
	url, err := db.WithDatabase(shared, name)
	if err != nil {
		t.Fatal(err)
	}
	return url
}
