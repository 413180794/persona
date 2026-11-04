package node

import (
	"context"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
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
	return filepath.Join(identity.SocketDir(home), "node-attach.sock")
}

// Relay pipes r / w to a resident node if one is listening. ok is false when there is none (the caller serves the
// connection itself); otherwise it returns when either side closes.
func Relay(ctx context.Context, r io.Reader, w io.Writer) (ok bool, err error) {
	c, err := dialResident(ctx, AttachSocketPath(), residentRestartWait)
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

// residentRestartWait: how long a relay waits for a resident node that is restarting (its socket is there, nobody
// answers: it is exec'ing a new binary, cmd/personad runResident) before serving the connection itself. Serving it
// here would put the assistant's conversations under this process, which ends with the hub's connection.
var residentRestartWait = 8 * time.Second

// dialResident dials the resident node's socket, again while it refuses (restarting) for up to wait; no socket at all
// (no resident node) returns at once.
func dialResident(ctx context.Context, path string, wait time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(wait)
	for {
		c, err := net.DialTimeout("unix", path, 2*time.Second)
		if err == nil || !errors.Is(err, syscall.ECONNREFUSED) || time.Now().After(deadline) {
			return c, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Attach runs a resident node: it listens on AttachSocketPath and serves one hub connection at a time, each relayed
// by `jarvisd node --stdio`, or arriving on extra (the hub dialing in over tailcat, tailcat.go; nil: none). A new
// connection replaces the one being served whichever way either came (the hub reconnected, maybe the other way; the
// old one may not have noticed yet), as a new ssh-started node used to replace the old one. newNode makes the Node
// for a connection. runtimes, when set, is the runtime socket (runtimehost.go): opened here, once this is the one
// resident node, and shared by every connection's Node, so runtimes connect before the hub does and stay connected
// while it reconnects.
func Attach(ctx context.Context, newNode func() (*Node, error), extra <-chan net.Conn, runtimes *RuntimeHost) error {
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
	if runtimes != nil {
		if err := runtimes.Listen(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "node: runtime socket: %v\n", err)
			runtimes = nil // each connection's Node opens its own, as without a resident node
		}
	}
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

	keeper := newAssistantKeeper()
	var served chan struct{} // closed when the connection being served has ended
	var current context.CancelFunc
	var wg sync.WaitGroup
	defer keeper.stopAll() // after the last connection handed them over: this resident node is ending
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
		prev, done := served, make(chan struct{})
		served = done
		go func() {
			defer wg.Done()
			defer close(done)
			defer c.Close()
			defer cancel()
			if prev != nil { // the last connection's Node hands its conversations over as it ends (assistant_keep.go)
				select {
				case <-prev:
				case <-time.After(10 * time.Second):
				}
			}
			n, err := newNode()
			if err != nil {
				fmt.Fprintf(os.Stderr, "node: %v\n", err)
				return
			}
			if runtimes != nil {
				n.runtimeHost = runtimes // Serve attaches it for this connection
			}
			n.keeper = keeper                        // conversations go on from one connection to the next (assistant_keep.go)
			go func() { <-cctx.Done(); c.Close() }() // a replaced connection stops reading at once
			if err := n.Serve(cctx, c, c); err != nil && !errors.Is(err, io.EOF) && cctx.Err() == nil {
				n.logger.Printf("connection ended: %v", err)
			}
		}()
	}
}
