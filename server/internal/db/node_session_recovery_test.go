package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const recoveryMachine = "legacy-linux-fixture"
const capturedSession = "legacy-unacknowledged"

func TestKnownLegacyRecoveryRestoresCapturedOldSenderStateOnce(t *testing.T) {
	d := open(t)
	ctx := t.Context()
	path := os.Getenv("FAN_LEGACY_RECOVERY_ARTIFACT")
	if path == "" {
		t.Skip("requires the captured synthetic old-resident artifact")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Sessions map[string]NodeSession `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	want := parsed.Sessions[capturedSession]
	if want.Pane != "sender-value" || want.StartedAt.IsZero() {
		t.Fatal("wrong legacy sender evidence")
	}
	before := want
	before.Pane = "prior-durable"
	if err := d.PutNodeSession(ctx, recoveryMachine, capturedSession, before); err != nil {
		t.Fatal(err)
	}
	known := fixtureRecovery(t, d, capturedSession, "31", want)
	known.EvidenceSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	// This represents a recovery whose caller did not receive the commit reply.
	if duplicate, err := d.RecoverKnownLegacyState(ctx, known); err != nil || duplicate {
		t.Fatalf("first apply: duplicate=%v %v", duplicate, err)
	}
	revision, err := d.NodeSessionReceipt(ctx, recoveryMachine, capturedSession)
	if err != nil || revision != known.Operation {
		t.Fatalf("missing revision after commit: %q %v", revision, err)
	}
	restored := readFixtureState(t, d, capturedSession)
	if restored.Pane != want.Pane {
		t.Fatalf("wrong recovery contents: %+v", restored)
	}
	newer := want
	newer.Pane = "later-legacy-write"
	if err := d.PutNodeSession(ctx, recoveryMachine, capturedSession, newer); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := d.RecoverKnownLegacyState(ctx, known); err != nil || !duplicate {
		t.Fatalf("lost reply retry must be idempotent: duplicate=%v %v", duplicate, err)
	}
	if current := readFixtureState(t, d, capturedSession); current.Pane != newer.Pane {
		t.Fatal("duplicate applied over newer legacy write")
	}
	known.State.Pane = "same-id-different-write"
	if _, err := d.RecoverKnownLegacyState(ctx, known); !errors.Is(err, ErrLegacyStateConflict) {
		t.Fatalf("operation identity reuse: %v", err)
	}
	t.Log("captured old-resident payload recovered once, durable receipt survived retry after a newer write")
}

func TestKnownLegacyRecoveryWaitsForAdmittedWriterAndRejectsStaleSnapshot(t *testing.T) {
	d := open(t)
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	id := "active-legacy"
	current := NodeSession{Launcher: "claude", Cwd: "/fixture", Pane: "before",
		StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := d.PutNodeSession(ctx, recoveryMachine, id, current); err != nil {
		t.Fatal(err)
	}
	captured := current
	captured.Pane = "attempted-recovery"
	recovery := fixtureRecovery(t, d, id, "32", captured)
	writer, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(ctx)
	current.Pane = "last-admitted-write"
	if err := putNodeSession(ctx, writer, recoveryMachine, id, current); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := d.RecoverKnownLegacyState(ctx, recovery)
		result <- err
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		err := d.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND pid <> pg_backend_pid()
			  AND wait_event_type='Lock' AND query LIKE 'SELECT session_id FROM node_sessions%FOR UPDATE')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("recovery returned while last admitted write was open: %v", err)
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("recovery never waited for admitted writer")
		}
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrLegacyStateConflict) {
		t.Fatalf("recovery accepted outdated predecessor: %v", err)
	}
	if state := readFixtureState(t, d, id); state.Pane != current.Pane {
		t.Fatalf("last admitted write lost: %+v", state)
	}
	if rev, err := d.NodeSessionReceipt(ctx, recoveryMachine, id); err != nil || rev != "" {
		t.Fatalf("rejected recovery changed revision: %q %v", rev, err)
	}
	t.Log("preexisting legacy transaction committed, stale recovery waited and then refused")
}

func TestKnownLegacyRecoveryFailsClosedWithoutEvidenceOrOnReadbackMismatch(t *testing.T) {
	d := open(t)
	ctx := t.Context()
	id := "captured-identity"
	before := NodeSession{Launcher: "claude", Cwd: "/fixture", Pane: "before", CreationID: "fixed-identity",
		StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := d.PutNodeSession(ctx, recoveryMachine, id, before); err != nil {
		t.Fatal(err)
	}
	wrong := before
	wrong.Pane, wrong.CreationID = "bad", "other-identity"
	known := fixtureRecovery(t, d, id, "33", wrong)
	if _, err := d.RecoverKnownLegacyState(ctx, known); !errors.Is(err, ErrLegacyStateConflict) {
		t.Fatalf("wrong persisted projection accepted: %v", err)
	}
	fingerprint, err := d.LegacyStateFingerprint(ctx, recoveryMachine, id)
	if err != nil || fingerprint != known.BeforeFingerprint {
		t.Fatal("failed readback partially committed")
	}
	known.EvidenceSHA256 = ""
	if _, err := d.RecoverKnownLegacyState(ctx, known); err == nil {
		t.Fatal("unidentified write accepted")
	}
	known = fixtureRecovery(t, d, id, "33", before)
	known.Session = "unknown-session"
	if _, err := d.RecoverKnownLegacyState(ctx, known); !errors.Is(err, ErrLegacyStateConflict) {
		t.Fatalf("missing existing session should stay unknown: %v", err)
	}
	t.Log("missing evidence, wrong resulting state and unknown session all rejected with no partial write")
}

func fixtureRecovery(t *testing.T, d *DB, id, op string, state NodeSession) KnownLegacyState {
	t.Helper()
	stamp, err := d.LegacyStateFingerprint(t.Context(), recoveryMachine, id)
	if err != nil {
		t.Fatal(err)
	}
	return KnownLegacyState{Machine: recoveryMachine, Session: id, Operation: strings.Repeat(op, 16),
		EvidenceSHA256: strings.Repeat("a", 64), BeforeFingerprint: stamp, State: state}
}

func readFixtureState(t *testing.T, d *DB, id string) NodeSession {
	t.Helper()
	rows, err := d.NodeSessions(t.Context(), recoveryMachine)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := rows[id]
	if !ok {
		t.Fatal("fixture missing")
	}
	return state
}
