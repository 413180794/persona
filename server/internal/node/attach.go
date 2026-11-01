package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A resident node (`jarvisd node --attach`, run by launchd in the user's login session on a Mac) and a relay
// (`jarvisd node --stdio`, what the hub starts over ssh) share the work: when a resident node is listening, the relay
// only pipes the ssh connection to it, and the resident node serves it. Everything the node starts (the assistant's
// claude, its key helper) then runs in the login session, where the login keychain is unlocked: an ssh session
// can't read it, so a claude whose API key comes from the keychain fails to authenticate there (docs/server.md).
//
// No resident node listening: --stdio serves the connection itself, as before. The hub doesn't know the difference.

// AttachSocketPath is where a resident node listens for relays.
func AttachSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis", "node-attach.sock")
}

// Relay pipes r / w to a resident node if one is listening. ok is false when there is none (the caller serves the
// connection itself); otherwise it returns when either side closes.
func Relay(ctx context.Context, r io.Reader, w io.Writer) (ok bool, err error) {
	c, err := net.DialTimeout("unix", AttachSocketPath(), 2*time.Second)
	if err != nil {
		return false, nil
	}
	defer c.Close()
	// hub -> node: when ssh's stdin ends, half-close so the node sees EOF and finishes. node -> hub decides when the
	// relay is over: everything the node wrote must reach the hub, even after the hub stopped sending.
	go func() { _, _ = io.Copy(c, r); _ = c.(*net.UnixConn).CloseWrite() }()
	back := make(chan error, 1)
	go func() { _, err := io.Copy(w, c); back <- err }()
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case err := <-back:
		return true, err
	}
}

// Attach runs a resident node: it listens on AttachSocketPath and serves one hub connection at a time, each relayed
// by `jarvisd node --stdio`, or arriving on extra (the hub dialing in over tailcat, tailcat.go; nil: none). A new
// connection replaces the one being served whichever way either came (the hub reconnected, maybe the other way; the
// old one may not have noticed yet), as a new ssh-started node used to replace the old one. newNode makes the Node
// for a connection.
func Attach(ctx context.Context, newNode func() (*Node, error), extra <-chan net.Conn) error {
	path := AttachSocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Only one resident node: a second one would steal relays from the first. A socket nobody answers on is stale.
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return fmt.Errorf("a resident node is already listening on %s", path)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	_ = os.Chmod(path, 0o600)
	ln.(*net.UnixListener).SetUnlinkOnClose(false) // a replacement may already own the path (hooks.go has the same trap)
	go func() { <-ctx.Done(); ln.Close() }()
	relayed := make(chan net.Conn)
	go func() {
		defer close(relayed)
		for {
			c, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			select {
			case relayed <- c:
			case <-ctx.Done():
				c.Close()
				return
			}
		}
	}()

	var current context.CancelFunc
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		var c net.Conn
		var ok bool
		select {
		case <-ctx.Done():
			return nil
		case c, ok = <-relayed:
			if !ok {
				return nil
			}
		case c, ok = <-extra:
			if !ok {
				extra = nil // that way is gone; relays go on
				continue
			}
		}
		if current != nil {
			current() // the previous connection's node shuts down; its hooks retry on the new one (hooks.go)
		}
		cctx, cancel := context.WithCancel(ctx)
		current = cancel
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer c.Close()
			defer cancel()
			n, err := newNode()
			if err != nil {
				fmt.Fprintf(os.Stderr, "node: %v\n", err)
				return
			}
			go func() { <-cctx.Done(); c.Close() }() // a replaced connection stops reading at once
			if err := n.Serve(cctx, c, c); err != nil && !errors.Is(err, io.EOF) && cctx.Err() == nil {
				n.logger.Printf("connection ended: %v", err)
			}
		}()
	}
}
