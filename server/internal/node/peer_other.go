//go:build !linux && !darwin

package node

import (
	"errors"
	"net"
)

var errNoPeer = errors.New("the process at the other end of a socket is not known on this system")

// peerPID: not known here, so no caller counts as the assistant.
func peerPID(net.Conn) (int, error) { return 0, errNoPeer }
