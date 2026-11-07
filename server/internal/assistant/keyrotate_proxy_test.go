package assistant

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/secretkey"
)

// pgProxy forwards a connection to Postgres (no TLS: the test database's sslmode=disable) and, armed, breaks it at the
// next COMMIT the way a network does:
//   - late: the client's side goes at once, while its COMMIT reaches the server only a little later (still on its way
//     when the client looks again);
//   - otherwise: the COMMIT goes through, and the server's answer never comes back.
type pgProxy struct {
	ln    net.Listener
	to    string
	late  bool
	armed atomic.Bool
	hit   atomic.Bool
}

func newPGProxy(t *testing.T, to string, late bool) *pgProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &pgProxy{ln: ln, to: to, late: late}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c)
		}
	}()
	return p
}

func (p *pgProxy) serve(c net.Conn) {
	s, err := net.Dial("tcp", p.to)
	if err != nil {
		c.Close()
		return
	}
	var mute atomic.Bool // the server's answers no longer reach the client
	var once sync.Once
	closeBoth := func() { once.Do(func() { c.Close(); s.Close() }) }
	defer closeBoth()
	go func() { // server -> client
		buf := make([]byte, 32<<10)
		for {
			n, err := s.Read(buf)
			if n > 0 && !mute.Load() {
				if _, werr := c.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				c.Close()
				return
			}
		}
	}()
	// client -> server: the startup message (no type byte), then typed messages
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return
	}
	body := make([]byte, binary.BigEndian.Uint32(hdr[:])-4)
	if _, err := io.ReadFull(c, body); err != nil {
		return
	}
	if _, err := s.Write(append(hdr[:], body...)); err != nil {
		return
	}
	for {
		var h [5]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			return
		}
		b := make([]byte, binary.BigEndian.Uint32(h[1:])-4)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		msg := append(h[:], b...)
		if h[0] == 'Q' && strings.HasPrefix(strings.ToLower(string(b)), "commit") && p.armed.CompareAndSwap(true, false) {
			p.hit.Store(true)
			mute.Store(true)
			if p.late {
				c.Close()                          // the client sees its connection go
				time.Sleep(500 * time.Millisecond) // while its COMMIT is still on its way
			}
			_, _ = s.Write(msg)
			if !p.late {
				c.Close() // the COMMIT went through; its answer never comes back
			}
			time.Sleep(500 * time.Millisecond) // the server finishes it before this side closes
			return
		}
		if _, err := s.Write(msg); err != nil {
			return
		}
	}
}

// A re-seal's connection that breaks at its COMMIT, the COMMIT still on its way or its answer lost: the rotation waits
// for the server's outcome and goes on from it; no secret is ever left sealed with a key that is not on disk.
func TestACommitWhoseConnectionBreaks(t *testing.T) {
	for _, late := range []bool{true, false} {
		name := "answer lost"
		if late {
			name = "commit still on its way"
		}
		t.Run(name, func(t *testing.T) {
			r := newRotRig(t)
			u, err := url.Parse(r.url)
			if err != nil {
				t.Fatal(err)
			}
			p := newPGProxy(t, u.Host, late)
			u.Host = p.ln.Addr().String()
			via, err := db.Open(context.Background(), u.String(), db.ManagerSet) // the rotating process
			if err != nil {
				t.Fatal(err)
			}
			defer via.Close()
			p.armed.Store(true)
			_, err = rotateSecretKey(context.Background(), via, r.home, r.key, r.logf)
			if !p.hit.Load() {
				t.Fatalf("the proxy saw no COMMIT (%v): %v", err, r.logs)
			}
			// What the services would read now opens (the start may also have stopped: then nothing reads).
			if err == nil {
				box := secretkey.NewWithFallback(r.key)
				for q, want := range rotPlain {
					if got, oerr := box.Open(r.value(q)); oerr != nil || got != want {
						t.Fatalf("%s: %q %v (%v)", q, got, oerr, r.logs)
					}
				}
			}
			via.Close()
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				if n, err := r.d.OtherManagers(context.Background()); err == nil && n == 0 {
					break // the rotating process's connections have left the server
				}
			}
			if err := r.rotate(); err != nil { // the next start
				t.Fatal(err)
			}
			r.rotated()
			if r.prevKept() {
				t.Fatalf("the old key stays: %v", r.logs)
			}
		})
	}
}
