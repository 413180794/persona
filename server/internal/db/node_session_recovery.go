package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/proto"
)

// KnownLegacyState is a previously captured sender snapshot. The caller must
// possess and independently verify the original evidence and a closed ingress.
type KnownLegacyState struct {
	Machine           string
	Session           string
	Operation         string
	EvidenceSHA256    string
	BeforeFingerprint string
	State             NodeSession
}

var ErrLegacyStateConflict = errors.New("legacy session recovery precondition changed")

func legacyStateHex(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size && hex.EncodeToString(b) == value
}

// LegacyStateFingerprint identifies the exact durable Manager state (including
// its write stamps), regardless of whether it was written by a legacy sender.
func (d *DB) LegacyStateFingerprint(ctx context.Context, machine, session string) (string, error) {
	return legacyStateFingerprint(ctx, d.pool, machine, session)
}

func legacyStateFingerprint(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, machine, session string) (string, error) {
	var durable string
	err := q.QueryRow(ctx, `SELECT jsonb_build_array(
		to_jsonb(n),
		(SELECT to_jsonb(s) FROM settings s WHERE key = 'node_model.' || $1 || '.' || $2),
		(SELECT to_jsonb(s) FROM settings s WHERE key = $3)
	)::text FROM node_sessions n WHERE machine_id=$1 AND session_id=$2`,
		machine, session, nodeSessionReceiptKey(machine, session)).Scan(&durable)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(durable))), nil
}

// RecoverKnownLegacyState conditionally repairs one existing session in a
// fenced maintenance database. The state, its v2 revision and the immutable
// recovery marker commit together. Repeating the same operation does not
// write, even if a later operation wrote new state after the first commit.
func (d *DB) RecoverKnownLegacyState(ctx context.Context, evidence KnownLegacyState) (alreadyApplied bool, err error) {
	return d.recoverLegacySession(ctx, identityOf(evidence), evidence, func(tx pgx.Tx) error {
		if err := putNodeSession(ctx, tx, evidence.Machine, evidence.Session, evidence.State); err != nil {
			return err
		}
		sessions, err := queryNodeSessions(ctx, tx, evidence.Machine)
		if err != nil {
			return err
		}
		stored, ok := sessions[evidence.Session]
		match, err := sameRecoveryState(stored, evidence.State)
		if err != nil {
			return err
		}
		if !ok || !match {
			return fmt.Errorf("%w: persisted state does not match captured snapshot", ErrLegacyStateConflict)
		}
		return nil
	})
}

// KnownLegacyPermissionMode is restricted to a single proved app-mode change.
// An independently verified accepted request is required to interpret a
// session.info projection as app state. The projection alone is ambiguous.
type KnownLegacyPermissionMode struct {
	Machine           string
	Session           string
	Operation         string
	EvidenceSHA256    string
	BeforeFingerprint string
	PreviousMode      string
	AcceptedMode      string
}

// RecoverKnownLegacyPermissionMode updates an existing, fingerprinted shadow
// row while ingress is closed. Neither a newer session nor unrelated fields
// in the JSON state are rewritten.
func (d *DB) RecoverKnownLegacyPermissionMode(ctx context.Context, evidence KnownLegacyPermissionMode) (bool, error) {
	if evidence.PreviousMode == evidence.AcceptedMode ||
		!slices.Contains(proto.PermissionModes, evidence.PreviousMode) ||
		!slices.Contains(proto.PermissionModes, evidence.AcceptedMode) {
		return false, errors.New("invalid known permission mode change")
	}
	return d.recoverLegacySession(ctx, legacyRecoveryIdentity{
		machine: evidence.Machine, session: evidence.Session, operation: evidence.Operation,
		evidenceSHA: evidence.EvidenceSHA256, before: evidence.BeforeFingerprint,
	}, evidence, func(tx pgx.Tx) error {
		var mode string
		err := tx.QueryRow(ctx, `UPDATE settings
			SET value=jsonb_set(value, '{app_mode}', to_jsonb($2::text), false), updated_at=now()
			WHERE key=$1 AND value->>'app_mode'=$3 RETURNING value->>'app_mode'`,
			"node_model."+evidence.Machine+"."+evidence.Session, evidence.AcceptedMode, evidence.PreviousMode).Scan(&mode)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && mode != evidence.AcceptedMode) {
			return ErrLegacyStateConflict
		}
		return err
	})
}

type legacyRecoveryIdentity struct {
	machine, session, operation, evidenceSHA, before string
}

func identityOf(e KnownLegacyState) legacyRecoveryIdentity {
	return legacyRecoveryIdentity{
		machine: e.Machine, session: e.Session, operation: e.Operation,
		evidenceSHA: e.EvidenceSHA256, before: e.BeforeFingerprint,
	}
}

func (d *DB) recoverLegacySession(ctx context.Context, id legacyRecoveryIdentity, evidence any, apply func(pgx.Tx) error) (bool, error) {
	if id.machine == "" || id.session == "" ||
		!legacyStateHex(id.operation, 16) || !legacyStateHex(id.evidenceSHA, 32) ||
		!legacyStateHex(id.before, 32) {
		return false, errors.New("incomplete legacy snapshot evidence")
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return false, err
	}
	binding := fmt.Sprintf("%x", sha256.Sum256(encoded))
	recoveryKey := "node_state_recovery." + id.operation
	receiptKey := nodeSessionReceiptKey(id.machine, id.session)
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	// Match the ordering in PutNodeSessionReceipt. The recovery key also
	// serializes an attempted operation-ID reuse against another session.
	for _, key := range []string{recoveryKey, receiptKey} {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
			return false, err
		}
	}
	previousRecovery, err := readNodeSessionReceipt(ctx, tx, recoveryKey)
	if err != nil {
		return false, err
	}
	if previousRecovery != "" {
		if previousRecovery != binding {
			return false, ErrLegacyStateConflict
		}
		return true, nil
	}

	// Legacy PutNodeSession does not acquire the v2 advisory lock. Its row
	// is nevertheless locked by the ordinary upsert; read the fingerprint
	// in a fresh statement after taking that lock.
	var session string
	err = tx.QueryRow(ctx, "SELECT session_id FROM node_sessions WHERE machine_id=$1 AND session_id=$2 FOR UPDATE",
		id.machine, id.session).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrLegacyStateConflict
	}
	if err != nil {
		return false, err
	}
	before, err := legacyStateFingerprint(ctx, tx, id.machine, id.session)
	if err != nil {
		return false, err
	}
	if before != id.before {
		return false, ErrLegacyStateConflict
	}
	if err := apply(tx); err != nil {
		return false, err
	}
	if err := writeNodeSessionReceipt(ctx, tx, receiptKey, id.operation); err != nil {
		return false, err
	}
	if err := writeNodeSessionReceipt(ctx, tx, recoveryKey, binding); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

func sameRecoveryState(stored, expected NodeSession) (bool, error) {
	normalize := func(s NodeSession) ([]byte, error) {
		s.StartedAt = s.StartedAt.UTC().Truncate(time.Microsecond)
		data, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var canonical any
		if err := decoder.Decode(&canonical); err != nil {
			return nil, err
		}
		return json.Marshal(canonical)
	}
	a, err := normalize(stored)
	if err != nil {
		return false, err
	}
	b, err := normalize(expected)
	return bytes.Equal(a, b), err
}
