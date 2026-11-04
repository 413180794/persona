//go:build !windows

package orcart

import (
	"context"
	"net"
)

// localKinds: Orca on macOS and Linux serves its runtime on a unix socket.
var localKinds = []string{kindUnix}

// dialPipe is never reached here: readMeta only picks localKinds.
func dialPipe(context.Context, string) (net.Conn, error) { return nil, ErrNotRunning }
