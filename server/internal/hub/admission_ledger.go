package hub

import (
	"context"
	"errors"
	"os"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// AdmissionLedgerCommitted follows the built-in Manager's configured primary.
// It neither starts a service nor migrates a database. A moved Manager must not
// be reconciled against the stale tables left on the shared Hub database.
func AdmissionLedgerCommitted(ctx context.Context, operation string) (bool, error) {
	cfg, err := runningHubConfig()
	if err != nil {
		return false, err
	}
	if cfg.Assistant.External || cfg.DatabaseURL == "" {
		return false, errors.New("no local built-in Manager database is configured")
	}
	return readAdmissionLedgerReceipt(ctx, cfg.dir, cfg.DatabaseURL, operation)
}

func readAdmissionLedgerReceipt(ctx context.Context, home, shared, operation string) (bool, error) {
	own, err := managerconf.ReadOwnDatabaseChecked(home)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("cannot read the built-in Manager database configuration")
	}
	url := own.URL
	if own.Name != "" {
		url, err = db.WithDatabase(shared, own.Name)
		if err != nil {
			return false, errors.New("cannot derive the built-in Manager database address")
		}
	}
	separate := url != ""
	if !separate {
		if !own.Moved.IsZero() || own.Container != "" {
			return false, errors.New("the built-in Manager's own database address is unavailable")
		}
		moved, err := db.ManagerMovedOut(ctx, shared)
		if err != nil || moved {
			return false, errors.New("cannot establish the shared database as the Manager's authority")
		}
		url = shared
	}
	database, err := db.Attach(ctx, url, db.ManagerSet)
	if err != nil {
		return false, errors.New("cannot connect to the configured Manager database")
	}
	defer database.Close()
	if separate {
		if _, moved, err := database.MovedIn(ctx); err != nil || !moved {
			return false, errors.New("the configured database has no proven Manager ownership")
		}
	}
	return database.SentRecordOperationCommitted(ctx, operation)
}
