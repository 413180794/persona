package assistant

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerconf"
)

// DropSharedCommand is `personad assistant drop-shared-tables [--yes]`: once the built-in manager's data moved into its
// own database (owndb.go), the hub's database still has the manager's tables as they were then, for a build from
// before switched back. This drops them there, only when asked: without --yes it lists what it would drop. Never
// automatic: a build from before switched back afterwards starts without the manager's data.
func DropSharedCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("assistant drop-shared-tables", flag.ContinueOnError)
	flags.SetOutput(stderr)
	yes := flags.Bool("yes", false, "drop them (without it: only list what would be dropped)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("assistant drop-shared-tables takes no arguments")
	}
	cfg, err := readHubConfig()
	if err != nil {
		return err
	}
	return dropShared(ctx, identity.HomeDir(), cfg.DatabaseURL, *yes, stdout)
}

func dropShared(ctx context.Context, home, shared string, yes bool, out io.Writer) error {
	own := managerconf.ReadOwnDatabase(home)
	if own.Moved.IsZero() || (own.URL == "" && own.Name == "") {
		return errors.New("the built-in manager's data has not moved into its own database: its tables in the hub's database are the ones in use")
	}
	if shared == "" {
		return errors.New("no database_url in hub.json (or PERSONA_DATABASE_URL)")
	}
	url := own.URL
	if own.Name != "" {
		var err error
		if url, err = db.WithDatabase(shared, own.Name); err != nil {
			return err
		}
	}
	mine, err := db.Attach(ctx, url, db.ManagerSet)
	if err != nil {
		return fmt.Errorf("its own database is not reachable (%w): nothing dropped", err)
	}
	defer mine.Close()
	if _, ok, err := mine.MovedIn(ctx); err != nil || !ok {
		return fmt.Errorf("its own database does not have its data (%v): nothing dropped", err)
	}
	theirs, err := db.Attach(ctx, shared, db.HubSet)
	if err != nil {
		return fmt.Errorf("the hub's database: %w", err)
	}
	defer theirs.Close()
	a, err := mine.Identity(ctx)
	if err != nil {
		return err
	}
	if b, err := theirs.Identity(ctx); err != nil || a == b {
		return errors.New("the hub's database is the manager's own (or cannot be told apart): nothing dropped")
	}
	ownTables, err := mine.Tables(ctx)
	if err != nil {
		return err
	}
	inShared, err := theirs.Tables(ctx)
	if err != nil {
		return err
	}
	hub := db.HubTables()
	var drop []string
	for _, t := range ownTables {
		// manager_moved stays: its 'out' keeps a start that does not know the move off the stale copy.
		if slices.Contains(inShared, t) && !slices.Contains(hub, t) && t != "manager_moved" {
			drop = append(drop, t)
		}
	}
	if len(drop) == 0 {
		fmt.Fprintln(out, "The hub's database has none of the manager's tables left.")
		return nil
	}
	fmt.Fprintf(out, "The hub's database keeps the built-in manager's tables as they were when its data moved (%s):\n", databaseMovedAt(own.Moved))
	for _, t := range drop {
		fmt.Fprintln(out, "  "+t)
	}
	if slices.Contains(drop, "settings") {
		fmt.Fprintln(out, "(settings also keeps the hub's own keys for a hub build from before hub_settings; this build keeps them in hub_settings.)")
	}
	if !yes {
		fmt.Fprintln(out, "Nothing dropped. With --yes they are dropped; a build from before switched back afterwards starts without the manager's data, and a hub from before hub_settings without its settings.")
		return nil
	}
	if err := theirs.DropTables(ctx, drop); err != nil {
		return err
	}
	fmt.Fprintf(out, "Dropped %d tables from the hub's database.\n", len(drop))
	return nil
}
