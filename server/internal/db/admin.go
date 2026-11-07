package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminSession is a browser signed in to the hub admin UI (docs/operations/admin.md). IDHash is the SHA-256 of the cookie value.
type AdminSession struct {
	IDHash    string
	CSRF      string
	CreatedAt time.Time
	ExpiresAt time.Time
	Source    string
	UserAgent string
}

// AdminPasswordHash is the administrator's password hash, "" while none was set.
func (d *DB) AdminPasswordHash(ctx context.Context) (string, error) {
	var hash string
	err := d.pool.QueryRow(ctx, `SELECT password_hash FROM admin_credentials WHERE id = 1`).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// SetAdminPassword stores the hash. With ifUnset it only sets the first password: false when one was already there
// (two browsers racing through first-run setup; only the first wins).
func (d *DB) SetAdminPassword(ctx context.Context, hash string, ifUnset bool) (bool, error) {
	q := `INSERT INTO admin_credentials (id, password_hash) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()`
	if ifUnset {
		q = `INSERT INTO admin_credentials (id, password_hash) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`
	}
	tag, err := d.pool.Exec(ctx, q, hash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ClearAdminPassword removes the password and every session: the next visit is first-run setup again.
func (d *DB) ClearAdminPassword(ctx context.Context) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM admin_credentials`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM admin_sessions`)
		return err
	})
}

func (d *DB) CreateAdminSession(ctx context.Context, s AdminSession) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO admin_sessions (id_hash, csrf, expires_at, source, user_agent) VALUES ($1, $2, $3, $4, $5)`,
		s.IDHash, s.CSRF, s.ExpiresAt, s.Source, s.UserAgent)
	if err == nil {
		// Expired sessions are only ever looked up to be refused: drop them while adding one.
		_, _ = d.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE expires_at <= now()`)
	}
	return err
}

// AdminSessionByHash is the unexpired session with that hash, nil when there is none.
func (d *DB) AdminSessionByHash(ctx context.Context, hash string) (*AdminSession, error) {
	var s AdminSession
	err := d.pool.QueryRow(ctx, `SELECT id_hash, csrf, created_at, expires_at, source, user_agent FROM admin_sessions
		WHERE id_hash = $1 AND expires_at > now()`, hash).Scan(&s.IDHash, &s.CSRF, &s.CreatedAt, &s.ExpiresAt, &s.Source, &s.UserAgent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) DeleteAdminSession(ctx context.Context, hash string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE id_hash = $1`, hash)
	return err
}

// DeleteAdminSessions signs every browser out but keep ("" for all of them).
func (d *DB) DeleteAdminSessions(ctx context.Context, keep string) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM admin_sessions WHERE id_hash <> $1`, keep)
	return err
}

// SetAdminPasswordSignOut replaces the password and signs every browser out but keep, in one transaction: a
// password change must not leave a session made with the old one.
func (d *DB) SetAdminPasswordSignOut(ctx context.Context, hash, keep string) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO admin_credentials (id, password_hash) VALUES (1, $1)
			ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()`, hash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM admin_sessions WHERE id_hash <> $1`, keep)
		return err
	})
}
