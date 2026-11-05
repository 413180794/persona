//go:build !windows

package herdr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// FullClient is a running full client: herdr's TUI in a PTY.
type FullClient struct {
	cmd  *exec.Cmd
	pty  *os.File
	done chan struct{}
	once sync.Once

	mu      sync.Mutex
	err     error
	closing bool
}

// StartFull runs `herdr session attach <session>` in a PTY of cols × rows and gives what it draws to onData, in order,
// from one goroutine. onData may block: nothing more is read from the PTY meanwhile, the client stops on a full
// buffer and so draws no more (this is the flow control); it must not keep the slice. Closing the client (or the
// client quitting) ends it; Done and Err say so.
func (c *Client) StartFull(opts FullOptions, onData func([]byte)) (*FullClient, error) {
	if opts.Session == "" || opts.Cols <= 0 || opts.Rows <= 0 {
		return nil, errors.New("herdr full client: session and size are needed")
	}
	cmd := exec.Command(c.Bin, "session", "attach", opts.Session)
	cmd.Env = fullClientEnv(os.Environ())
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(opts.Rows), Cols: uint16(opts.Cols)})
	if err != nil {
		return nil, fmt.Errorf("herdr session attach: %w", err)
	}
	fc := &FullClient{cmd: cmd, pty: f, done: make(chan struct{})}
	go func() {
		defer close(fc.done)
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				onData(buf[:n])
			}
			if err != nil { // EIO once the client has gone, EOF or a closed file otherwise
				break
			}
		}
		werr := cmd.Wait()
		fc.mu.Lock()
		defer fc.mu.Unlock()
		if !fc.closing {
			fc.err = exitReason(werr)
		}
		_ = f.Close()
	}()
	return fc, nil
}

func exitReason(err error) error {
	if err == nil {
		return errors.New("herdr client exited")
	}
	return fmt.Errorf("herdr client exited: %v", strings.TrimPrefix(err.Error(), "exit status "))
}

// Write types into the client (what the browser's keys and pastes produce).
func (c *FullClient) Write(p []byte) error {
	select {
	case <-c.done:
		return io.ErrClosedPipe
	default:
	}
	_, err := c.pty.Write(p)
	return err
}

// Resize gives the client's PTY a new grid; the kernel tells the client (SIGWINCH).
func (c *FullClient) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("bad size")
	}
	return pty.Setsize(c.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Done closes when the client is gone; Err then says why (nil after Close).
func (c *FullClient) Done() <-chan struct{} { return c.done }

func (c *FullClient) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the client and waits for it to be gone: hanging up its terminal first (herdr's client exits on that and
// leaves the server and its panes alone), then TERM and KILL to its process group when it does not.
func (c *FullClient) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		_ = c.pty.Close()
		for _, step := range []struct {
			sig  syscall.Signal
			wait time.Duration
		}{{0, 2 * time.Second}, {syscall.SIGTERM, 2 * time.Second}, {syscall.SIGKILL, 5 * time.Second}} {
			if step.sig != 0 && c.cmd.Process != nil {
				_ = syscall.Kill(-c.cmd.Process.Pid, step.sig) // the client leads its own session, so its group
			}
			select {
			case <-c.done:
				return
			case <-time.After(step.wait):
			}
		}
	})
}
