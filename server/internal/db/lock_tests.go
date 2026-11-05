package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type LockTest struct {
	TestID            string     `json:"test_id"`
	DeviceID          string     `json:"device_id"`
	DeviceSessionID   *int64     `json:"device_session_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	DueAt             time.Time  `json:"due_at"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	ReceivedAt        *time.Time `json:"received_at,omitempty"`
	NotificationShown *bool      `json:"notification_shown,omitempty"`
	Status            string     `json:"status"`
}

func (d *DB) CreateLockTest(ctx context.Context, t LockTest) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO lock_tests (test_id, device_id, created_at, due_at)
		VALUES ($1, $2, $3, $4)`, t.TestID, t.DeviceID, t.CreatedAt, t.DueAt)
	return err
}

const lockTestColumns = `test_id, device_id, device_session_id, created_at, due_at,
	sent_at, received_at, notification_shown, status`

func scanLockTest(row pgx.Row) (*LockTest, error) {
	var t LockTest
	err := row.Scan(&t.TestID, &t.DeviceID, &t.DeviceSessionID, &t.CreatedAt, &t.DueAt,
		&t.SentAt, &t.ReceivedAt, &t.NotificationShown, &t.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

func (d *DB) LockTest(ctx context.Context, deviceID, testID string) (*LockTest, error) {
	return scanLockTest(d.pool.QueryRow(ctx, `SELECT `+lockTestColumns+` FROM lock_tests
		WHERE device_id = $1 AND test_id = $2`, deviceID, testID))
}

func (d *DB) DueLockTests(ctx context.Context, at time.Time) ([]LockTest, error) {
	rows, err := d.pool.Query(ctx, `SELECT `+lockTestColumns+` FROM lock_tests
		WHERE status = 'scheduled' AND due_at <= $1 ORDER BY due_at LIMIT 100`, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LockTest
	for rows.Next() {
		t, err := scanLockTest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// NextLockTestDue returns the earliest due time of a scheduled test, or nil when nothing is scheduled.
func (d *DB) NextLockTestDue(ctx context.Context) (*time.Time, error) {
	var next *time.Time
	err := d.pool.QueryRow(ctx, `SELECT min(due_at) FROM lock_tests WHERE status = 'scheduled'`).Scan(&next)
	return next, err
}

// AttemptLockTest claims a single delivery attempt before enqueueing it. Only a receipt proves delivery.
// A zero session means that no capable connection was available at the scheduled time.
func (d *DB) AttemptLockTest(ctx context.Context, testID string, sessionID int64, at time.Time) (bool, error) {
	result, err := d.pool.Exec(ctx, `UPDATE lock_tests SET
		status = CASE WHEN $2 = 0 THEN 'unavailable' ELSE 'sent' END,
		device_session_id = NULLIF($2, 0), sent_at = CASE WHEN $2 = 0 THEN NULL ELSE $3 END
		WHERE test_id = $1 AND status = 'scheduled' AND due_at <= $3
		AND ($2 = 0 OR EXISTS (SELECT 1 FROM device_sessions s
			WHERE s.id = $2 AND s.device_id = lock_tests.device_id AND s.disconnected_at IS NULL))`,
		testID, sessionID, at)
	return result.RowsAffected() == 1, err
}

// ReceiveLockTest preserves the first receipt so retries cannot rewrite the observed result.
func (d *DB) ReceiveLockTest(ctx context.Context, deviceID, testID string, shown *bool, at time.Time) (*LockTest, error) {
	return scanLockTest(d.pool.QueryRow(ctx, `UPDATE lock_tests SET
		status = 'received', received_at = COALESCE(received_at, $4),
		notification_shown = CASE WHEN received_at IS NULL THEN $3 ELSE notification_shown END
		WHERE device_id = $1 AND test_id = $2 AND status IN ('sent', 'received')
		RETURNING `+lockTestColumns, deviceID, testID, shown, at))
}

// RecoverLockTests never delivers an overdue test after downtime as if it had arrived on time.
func (d *DB) RecoverLockTests(ctx context.Context, at time.Time) error {
	_, err := d.pool.Exec(ctx, `UPDATE lock_tests SET status = 'interrupted'
		WHERE status = 'scheduled' AND due_at <= $1`, at)
	return err
}

func (d *DB) PruneLockTests(ctx context.Context, before time.Time) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM lock_tests WHERE created_at < $1`, before)
	return err
}
