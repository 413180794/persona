package orcart

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/tailscale/go-winio"
)

// localKinds: Orca on Windows serves its runtime on a named pipe; a unix socket is taken too if a build lists one.
var localKinds = []string{kindNamedPipe, kindUnix}

// dialPipe connects to Orca's named pipe. A pipe that does not exist means Orca is not running (it left its metadata
// file behind); one that stays busy until ctx ends means a wedged app.
func dialPipe(ctx context.Context, pipe string) (net.Conn, error) {
	conn, err := winio.DialPipeContext(ctx, pipe)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: pipe: %v", ErrNotRunning, os.ErrNotExist)
		}
		return nil, fmt.Errorf("orca: connect: %w", err)
	}
	return conn, nil
}
