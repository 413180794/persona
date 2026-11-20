package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/nwverse/persona/server/internal/admission"
)

var ErrLedgerAuthorityMismatch = errors.New("ledger database does not match the bound Manager primary")

type ledgerQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// LedgerAuthority reads the identity of the Manager's own connected primary.
func (d *DB) LedgerAuthority(ctx context.Context) (admission.LedgerAuthority, error) {
	return readLedgerAuthority(ctx, d.pool)
}

func readLedgerAuthority(ctx context.Context, q ledgerQuery) (admission.LedgerAuthority, error) {
	var authority admission.LedgerAuthority
	var primary bool
	err := q.QueryRow(ctx, `SELECT system_identifier::text, oid, NOT pg_catalog.pg_is_in_recovery()
		FROM pg_catalog.pg_control_system(), pg_catalog.pg_database
		WHERE datname = pg_catalog.current_database()`).Scan(
		&authority.SystemIdentifier, &authority.DatabaseOID, &primary)
	if err != nil {
		return admission.LedgerAuthority{}, err
	}
	if !primary {
		return admission.LedgerAuthority{}, ErrLedgerAuthorityMismatch
	}
	return authority, nil
}

func verifyLedgerAuthority(ctx context.Context, q ledgerQuery, expected admission.LedgerAuthority) error {
	actual, err := readLedgerAuthority(ctx, q)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrLedgerAuthorityMismatch
	}
	return nil
}

// SentRecordOperationCommitted checks the frozen authority and reads the
// receipt in one read-only transaction, so both queries use the same session.
func (d *DB) SentRecordOperationCommitted(ctx context.Context, operation admission.LedgerOperation) (bool, error) {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := verifyLedgerAuthority(ctx, tx, operation.Authority); err != nil {
		return false, err
	}
	var present bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM sent_record_operations WHERE operation_id = $1)`, operation.ID).Scan(&present); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return present, nil
}
