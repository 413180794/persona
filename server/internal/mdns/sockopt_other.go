//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package mdns

import "syscall"

// reuseControl does nothing here: binding 5353 works only when no other responder holds it.
func reuseControl(_, _ string, _ syscall.RawConn) error { return nil }
