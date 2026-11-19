package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	if evidence.Machine == "" || evidence.Session == "" ||
		!legacyStateHex(evidence.Operation, 16) || !legacyStateHex(evidence.EvidenceSHA256, 32) ||
		!legacyStateHex(evidence.BeforeFingerprint, 32) {
		return false, errors.New("incomplete legacy snapshot evidence")
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return false, err
	}
	binding := fmt.Sprintf("%x", sha256.Sum256(encoded))
	recoveryKey := "node_state_recovery." + evidence.Operation
	receiptKey := nodeSessionReceiptKey(evidence.Machine, evidence.Session)
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
		evidence.Machine, evidence.Session).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrLegacyStateConflict
	}
	if err != nil {
		return false, err
	}
	before, err := legacyStateFingerprint(ctx, tx, evidence.Machine, evidence.Session)
	if err != nil {
		return false, err
	}
	if before != evidence.BeforeFingerprint {
		return false, ErrLegacyStateConflict
	}
	if err := putNodeSession(ctx, tx, evidence.Machine, evidence.Session, evidence.State); err != nil {
		return false, err
	}
	sessions, err := queryNodeSessions(ctx, tx, evidence.Machine)
	if err != nil {
		return false, err
	}
	stored, ok := sessions[evidence.Session]
	match, err := sameRecoveryState(stored, evidence.State)
	if err != nil {
		return false, err
	}
	if !ok || !match {
		return false, fmt.Errorf("%w: persisted state does not match captured snapshot", ErrLegacyStateConflict)
	}
	if err := writeNodeSessionReceipt(ctx, tx, receiptKey, evidence.Operation); err != nil {
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
