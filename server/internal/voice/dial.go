package voice

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// credential is the provider's Bearer credential: Key, the first line of KeyFile, or the first line KeyCommand
// prints (run for every conversation, so a credential renewed elsewhere is picked up). None is allowed.
func (p *Provider) credential(ctx context.Context) (string, error) {
	switch {
	case p.Key != "":
		return p.Key, nil
	case p.KeyFile != "":
		raw, err := os.ReadFile(expandHome(p.KeyFile))
		if err != nil {
			return "", fmt.Errorf("%s: key_file: %w", p.ID, err)
		}
		return firstLine(raw), nil
	case p.KeyCommand != "":
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "sh", "-c", p.KeyCommand)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("%s: key_command: %v %s", p.ID, err, strings.TrimSpace(stderr.String()))
		}
		if key := firstLine(out); key != "" {
			return key, nil
		}
		return "", fmt.Errorf("%s: key_command printed nothing", p.ID)
	}
	return "", nil
}

func firstLine(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(line)
}

// httpClient reaches the provider: directly, or through its SSH machine.
func (p *Provider) httpClient() *http.Client {
	if p.SSH == "" {
		return &http.Client{}
	}
	return &http.Client{Transport: &http.Transport{DialContext: sshDialer(p.SSH), TLSHandshakeTimeout: 15 * time.Second}}
}

// sshDialer opens a connection to addr from another machine: `ssh -W addr target`, its stdio as the connection.
func sshDialer(target string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(_ context.Context, _, addr string) (net.Conn, error) {
		// Not tied to ctx: the connection outlives the dial. A failed ssh shows on the first read, with its stderr
		// (for TLS the server only talks after the client, so the dial cannot wait for a first byte).
		cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15",
			"-W", addr, target)
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr := &tailBuffer{}
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("ssh %s: %w", target, err)
		}
		return &cmdConn{cmd: cmd, in: in, out: bufio.NewReader(out), stderr: stderr, addr: addr}, nil
	}
}

// cmdConn is a net.Conn over a command's stdin and stdout.
type cmdConn struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *tailBuffer
	addr   string
	once   sync.Once
}

func (c *cmdConn) Read(b []byte) (int, error) {
	n, err := c.out.Read(b)
	if err == io.EOF && n == 0 {
		if msg := strings.TrimSpace(c.stderr.String()); msg != "" {
			return 0, fmt.Errorf("ssh: %s", msg)
		}
	}
	return n, err
}

func (c *cmdConn) Write(b []byte) (int, error) { return c.in.Write(b) }

func (c *cmdConn) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
	})
	return nil
}

func (c *cmdConn) LocalAddr() net.Addr              { return sshAddr("ssh") }
func (c *cmdConn) RemoteAddr() net.Addr             { return sshAddr(c.addr) }
func (c *cmdConn) SetDeadline(time.Time) error      { return nil }
func (c *cmdConn) SetReadDeadline(time.Time) error  { return nil }
func (c *cmdConn) SetWriteDeadline(time.Time) error { return nil }

type sshAddr string

func (a sshAddr) Network() string { return "ssh" }
func (a sshAddr) String() string  { return string(a) }

// tailBuffer keeps the last 2 KB written to it.
type tailBuffer struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 2048 {
		t.b = t.b[len(t.b)-2048:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

var errNoProvider = errors.New("no such provider")

// errNoUpstream: no upstream is set up yet (Config.Setup says what is missing).
var errNoUpstream = errors.New("no realtime voice service is set up")
