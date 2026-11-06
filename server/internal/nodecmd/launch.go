package nodecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/nwverse/persona/server/internal/node"
	"github.com/nwverse/persona/server/internal/nodeclient"
)

// LaunchNodeWait is how long `personad launch` waits for the local node to hand over a launcher's settings. The node
// answers from memory, so a healthy one takes milliseconds; one that accepts the connection and never answers
// (restarting, stuck, or a stale socket held by another process) would otherwise keep the pane blank forever (#4548).
var LaunchNodeWait = 10 * time.Second

// runLaunch is `personad launch <launcher> [claude args…]`. stopSignals undoes main's SIGINT / SIGTERM handling
// first: launch has nothing to clean up (it execs claude right away), so Ctrl-C or a kill while it waits for the node
// should end it the way the process's inherited handling says, not sit in a context nobody reads.
func runLaunch(stopSignals func(), name string, args []string) error {
	stopSignals()
	ctx, cancel := context.WithTimeout(context.Background(), LaunchNodeWait)
	defer cancel()
	err := node.Launch(ctx, name, args)
	if err != nil && (errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		return fmt.Errorf("%s launch %s: the local node did not answer within %s (socket %s); "+
			"restart the Persona node on this computer, or run `%s node doctor` to see what is wrong",
			CommandName(), name, LaunchNodeWait, nodeclient.SocketPath(), CommandName())
	}
	return err
}
