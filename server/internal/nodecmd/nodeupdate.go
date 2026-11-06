package nodecmd

import (
	"context"
	"errors"
	"os"

	"github.com/nwverse/persona/server/internal/nodeupdate"
)

func runNodeUpdateWatch(ctx context.Context, args []string) error {
	if len(args) == 3 && args[2] == "--managed-ready" {
		return nodeupdate.WatchManaged(ctx, args[0], args[1])
	}
	if len(args) != 2 {
		return errors.New("usage: " + CommandName() + " node-update-watch <pending> <id>")
	}
	ready := os.NewFile(3, "node-watchdog-ready")
	if ready != nil {
		defer ready.Close()
	}
	return nodeupdate.Watch(ctx, args[0], args[1], ready)
}
