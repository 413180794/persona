package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// The default manager moves its data out of the database it shared with the hub into one of its own, once
// (docs/server/server.md "The default manager's own database"). Nothing may write its tables in the shared database while it
// copies them: every manager running there holds the share of an advisory lock (HoldManagerShare), and the move takes
// it alone (LockManagerMove), after the processes of a build from before (which hold none, and give no
// application_name) are gone from the database.

// managerMoveLock is the advisory lock's key: "persona manager move".
const managerMoveLock int64 = 0x706d6d6f7665

// ErrManagerBusy: another process uses the manager's tables in the shared database; the move waits for a later start.
var ErrManagerBusy = errors.New("another process still uses the manager's tables in this database")

// ShareHold is the advisory lock's share held on a connection of its own (not the pool's: it outlives the manager's
// database handle, and its loss is noticed).
type ShareHold struct {
	url  string
	mu   sync.Mutex
	conn *pgx.Conn
}

// HoldManagerShare holds the advisory lock's share on a connection of its own to the database at url, until Release:
// a manager running on the database it shares with the hub, so a move by another process waits for it.
func HoldManagerShare(ctx context.Context, url string) (*ShareHold, error) {
	h := &ShareHold{url: url}
	if err := h.take(ctx); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *ShareHold) take(ctx context.Context) error {
	cfg, err := pgx.ParseConfig(h.url)
	if err != nil {
		return err
	}
	if _, set := cfg.RuntimeParams["application_name"]; !set {
		cfg.RuntimeParams["application_name"] = AppManager
	}
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	if _, err := c.Exec(ctx, `SELECT pg_advisory_lock_shared($1)`, managerMoveLock); err != nil {
		_ = c.Close(context.Background())
		return err
	}
	h.mu.Lock()
	h.conn = c
	h.mu.Unlock()
	return nil
}

// Check makes sure the share is still held: its connection answers, else it is taken again on a new one (the server
// restarted). An error: it is not held now.
func (h *ShareHold) Check(ctx context.Context) error {
	h.mu.Lock()
	c := h.conn
	h.mu.Unlock()
	if c != nil && c.Ping(ctx) == nil {
		return nil
	}
	if c != nil {
		_ = c.Close(context.Background())
	}
	return h.take(ctx)
}

// Release lets it go.
func (h *ShareHold) Release() {
	h.mu.Lock()
	c := h.conn
	h.conn = nil
	h.mu.Unlock()
	if c != nil {
		_ = c.Close(context.Background()) // the share goes with the session
	}
}

// LockManagerMove takes the advisory lock alone, waiting up to wait for the managers holding its share to stop and
// for the connections of a build from before (no application_name) to leave the database; ErrManagerBusy when they
// did not. unlock lets it go.
func (d *DB) LockManagerMove(ctx context.Context, wait time.Duration) (unlock func(), err error) {
	c, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		var got bool
		if err := c.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, managerMoveLock).Scan(&got); err != nil {
			c.Release()
			return nil, err
		}
		if got {
			var older int
			if err := c.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
				AND pid <> pg_backend_pid() AND backend_type = 'client backend' AND coalesce(application_name, '') = ''`).Scan(&older); err != nil {
				_, _ = c.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, managerMoveLock)
				c.Release()
				return nil, err
			}
			if older == 0 {
				return func() {
					_, _ = c.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, managerMoveLock)
					c.Release()
				}, nil
			}
			_, _ = c.Exec(ctx, `SELECT pg_advisory_unlock($1)`, managerMoveLock)
		}
		if !time.Now().Before(deadline) {
			c.Release()
			return nil, fmt.Errorf("%w (waited %s)", ErrManagerBusy, wait)
		}
		select {
		case <-ctx.Done():
			c.Release()
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// HasManagerData: the database at url has the manager's tables (its set was applied there: a database the hub shared
// with the default manager before it had one of its own). It migrates nothing.
func HasManagerData(ctx context.Context, url string) (bool, error) {
	pool, err := connect(ctx, url, AppManager)
	if err != nil {
		return false, err
	}
	defer pool.Close()
	var has bool
	err = pool.QueryRow(ctx, `SELECT to_regclass('settings') IS NOT NULL OR to_regclass('todos') IS NOT NULL`).Scan(&has)
	return has, err
}

// ManagerMovedOut: the database at url (one the hub shared with the default manager) says the manager's data left it
// (MarkMovedOut). It migrates nothing: false when the record's table is not there.
func ManagerMovedOut(ctx context.Context, url string) (bool, error) {
	pool, err := connect(ctx, url, AppManager)
	if err != nil {
		return false, err
	}
	defer pool.Close()
	var out bool
	err = pool.QueryRow(ctx, `SELECT to_regclass('manager_moved') IS NOT NULL AND EXISTS (SELECT 1 FROM manager_moved WHERE direction = 'out')`).Scan(&out)
	if err != nil {
		var has bool
		if e := pool.QueryRow(ctx, `SELECT to_regclass('manager_moved') IS NOT NULL`).Scan(&has); e == nil && !has {
			return false, nil
		}
	}
	return out, err
}
