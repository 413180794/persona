//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package mdns

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// reuseControl lets the socket share port 5353 with the system's responder (mDNSResponder on macOS, avahi on
// Linux), which binds it the same way; every socket on the port then gets a copy of each multicast packet.
func reuseControl(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); serr != nil {
			return
		}
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
