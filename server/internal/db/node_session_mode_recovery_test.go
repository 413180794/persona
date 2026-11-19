package db

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKnownLegacyPermissionModeRecoversOnlyProvedField(t *testing.T) {
	path := os.Getenv("FAN_LEGACY_MODE_PROJECTION")
	if path == "" {
		t.Skip("requires the synthetic projection from the exact-ELF reconnect test")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Accepted bool   `json:"legacy_rpc_succeeded"`
		Dropped  bool   `json:"dropped_state_put"`
		Mode     string `json:"read_only_projection_before_disconnect"`
		After    string `json:"read_only_projection_after_same_elf_and_pid_reconnect"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil || !projection.Accepted ||
		!projection.Dropped || projection.Mode != "plan" || projection.After != "default" {
		t.Fatalf("incorrect exact-ELF synthetic evidence: %v", err)
	}
	d := open(t)
	ctx := t.Context()
	session := "face4444-1111-4222-8333-abcdef123456"
	original := NodeSession{
		Launcher: "claude", Cwd: "/fixture/.persona/assistant", Pane: "keep-pane",
		AppMode: projection.After, AppEffort: "high", StartedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := d.PutNodeSession(ctx, recoveryMachine, session, original); err != nil {
		t.Fatal(err)
	}
	key := "node_model." + recoveryMachine + "." + session
	if _, err := d.pool.Exec(ctx, `UPDATE settings SET value=value||'{"future_field":{"stay":true}}' WHERE key=$1`, key); err != nil {
		t.Fatal(err)
	}
	readUnchanged := func() string {
		t.Helper()
		var snapshot string
		err := d.pool.QueryRow(ctx, `SELECT jsonb_build_array(to_jsonb(n),s.value-'app_mode')::text
			FROM node_sessions n JOIN settings s ON s.key=$3 WHERE n.machine_id=$1 AND n.session_id=$2`,
			recoveryMachine, session, key).Scan(&snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	stable := readUnchanged()
	fp, err := d.LegacyStateFingerprint(ctx, recoveryMachine, session)
	if err != nil {
		t.Fatal(err)
	}
	evidence := KnownLegacyPermissionMode{
		Machine: recoveryMachine, Session: session, Operation: strings.Repeat("ab", 16),
		EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), BeforeFingerprint: fp,
		PreviousMode: projection.After, AcceptedMode: projection.Mode,
	}
	stale := evidence
	stale.BeforeFingerprint = strings.Repeat("b", 64)
	if _, err := d.RecoverKnownLegacyPermissionMode(ctx, stale); !errors.Is(err, ErrLegacyStateConflict) {
		t.Fatalf("stale predecessor was not rejected: %v", err)
	}
	if repeat, err := d.RecoverKnownLegacyPermissionMode(ctx, evidence); repeat || err != nil {
		t.Fatalf("recover: repeat=%t, %v", repeat, err)
	}
	if got := readFixtureState(t, d, session); got.AppMode != projection.Mode || stable != readUnchanged() {
		t.Fatal("mode recovery missed the new value or modified unrelated state")
	}
	if receipt, err := d.NodeSessionReceipt(ctx, recoveryMachine, session); receipt != evidence.Operation || err != nil {
		t.Fatalf("missing durable operation marker: %q %v", receipt, err)
	}
	later := original
	later.AppMode = "auto"
	later.Pane = "new-owner-value"
	if err := d.PutNodeSession(ctx, recoveryMachine, session, later); err != nil {
		t.Fatal(err)
	}
	if repeat, err := d.RecoverKnownLegacyPermissionMode(ctx, evidence); !repeat || err != nil {
		t.Fatalf("idempotent recovery readback: repeat=%t, %v", repeat, err)
	}
	if got := readFixtureState(t, d, session); got.AppMode != later.AppMode || got.Pane != later.Pane {
		t.Fatal("late retry overwrote a newer write")
	}
	t.Log("same-connection evidence repaired the uncommitted app_mode without touching other fields or later writes")
}
