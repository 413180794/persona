// Package bus is how a service reaches the hub (docs/protocol/protocol.md "Bus"): it dials GET /api/v1/bus with a token of
// its own and keeps the connection up. Over it runs the internal/rpc protocol unchanged, one
// JSON line per WebSocket text message, calls going both ways. Client is the service's side; the hub's is
// hub/bus.go.
package bus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// Path is where the hub accepts the services' connections.
	Path = "/api/v1/bus"
	// JoinPath is where a new manager trades its one-time install code for its token (POST {"code"} → {id, name,
	// token, hub_id}; hub/managers.go). It needs no credential.
	JoinPath = "/api/v1/bus/join"

	// RoleManager is the role of a token that speaks for a manager: a process that owns business state, such as the
	// assistant. It is the only role so far.
	RoleManager = "manager"

	// MaxMessage is the largest RPC line either side takes.
	MaxMessage = 64 << 20
)

// Both sides ping every PingEvery and drop the connection after PingTimeout without a pong: a laptop that went to
// sleep leaves a TCP connection that looks open for a long time. The hub drops a connection that has not completed
// its handshake within HelloTimeout. These are settings, not measurements.
const (
	PingEvery    = 30 * time.Second
	PingTimeout  = 20 * time.Second
	HelloTimeout = 40 * time.Second
)

// Error codes of the bus's own refusals (rpc.Error.Code).
const (
	CodeWrongIdentity   = "wrong_identity"   // the handshake names another identity than the token's: the hub hangs up
	CodeStaleConnection = "stale_connection" // no handshake on this connection yet, or a newer one replaced it
	// CodeTokenRevoked is the code of the hub's 401 to a bus token it has no identity for (revoked or replaced): the only
	// 401 a service takes for ErrRevoked.
	CodeTokenRevoked = "token_revoked"
	// CodeStoreUnavailable is the code of the hub's 503 when it cannot look tokens up (its database): try again.
	CodeStoreUnavailable = "store_unavailable"
	CodeUnknownMethod    = "unknown_method"
	CodeInvalidParams    = "invalid_params"
)

// Close codes besides the standard ones (1001: the hub is stopping), as on the computer link (internal/link).
const (
	CloseReplaced websocket.StatusCode = 4001 // a newer connection of the same identity took over
	CloseRevoked  websocket.StatusCode = 4002 // the token was revoked or replaced
	CloseRefused  websocket.StatusCode = 4003 // the handshake was wrong or did not come in time
)

// ErrRejected: the hub refused the token (401), or closed the connection because the token is no longer good or the
// handshake does not match it. Retrying with the same token only helps once someone fixes it on the hub, so Client
// waits its longest backoff between tries.
var ErrRejected = errors.New("the hub rejected the token")

// ErrRevoked is ErrRejected for the token itself, on the hub's explicit word only: it hung up because the token was
// revoked or replaced (CloseRevoked), or refused the dial with 401 code CodeTokenRevoked (a bus token it has no
// identity for). A plain 401, or a hub that cannot check tokens right now (503 CodeStoreUnavailable), is not it. Only a
// new token helps (a manager made in the app: a new install code, `personad manager join`). A Client with
// StopWhenRevoked returns it from Run instead of trying again.
var ErrRevoked = fmt.Errorf("%w: it was revoked or replaced", ErrRejected)

// Conn adapts a WebSocket to the io.Reader and io.Writer an rpc.Peer serves on: each line written goes out as one
// text message, each message read comes back as one line.
type Conn struct {
	ws  *websocket.Conn
	ctx context.Context

	rmu sync.Mutex
	buf []byte // the rest of the message being read, its newline included

	wmu  sync.Mutex
	wbuf []byte // a line written in pieces, until its newline
}

// NewConn wraps ws; ctx bounds every read and write (canceling it closes the connection without a close frame).
func NewConn(ctx context.Context, ws *websocket.Conn) *Conn {
	ws.SetReadLimit(MaxMessage)
	return &Conn{ws: ws, ctx: ctx}
}

func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.buf) == 0 {
		typ, msg, err := c.ws.Read(c.ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return 0, io.EOF
			}
			return 0, err
		}
		if typ != websocket.MessageText {
			continue
		}
		c.buf = append(msg, '\n')
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

func (c *Conn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.wbuf = append(c.wbuf, p...)
	for {
		i := bytes.IndexByte(c.wbuf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := c.wbuf[:i]
		if len(bytes.TrimSpace(line)) > 0 {
			if err := c.ws.Write(c.ctx, websocket.MessageText, line); err != nil {
				c.wbuf = c.wbuf[:0]
				return 0, err
			}
		}
		c.wbuf = c.wbuf[i+1:]
	}
}

// Close ends the connection with a normal closure (the other side reads io.EOF).
func (c *Conn) Close() error { return c.ws.Close(websocket.StatusNormalClosure, "") }

// Keepalive pings every interval and returns when a pong does not come back within timeout, or when ctx ends. The
// connection must be being read for pongs to arrive (an rpc peer serving on it always is).
func Keepalive(ctx context.Context, ws *websocket.Conn, every, timeout time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, timeout)
		err := ws.Ping(pctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("no pong within %s: %w", timeout, err)
		}
	}
}

// Dial connects to the hub's bus with a service's token. hub is the hub's base URL (http(s)://host:port, the same
// one the apps use); client may be nil (a pinned TLS client for the hub's public port otherwise).
func Dial(ctx context.Context, hub, token string, client *http.Client) (*websocket.Conn, error) {
	u, err := Endpoint(hub)
	if err != nil {
		return nil, err
	}
	ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + token}},
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			if errorCode(resp) == CodeTokenRevoked {
				return nil, ErrRevoked
			}
			return nil, ErrRejected
		}
		return nil, err
	}
	return ws, nil
}

// errorCode is the "code" of a refused dial's JSON error body ("" when there is none).
func errorCode(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	var e struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&e)
	return e.Code
}

// Endpoint is the bus's URL on the hub at base URL hub.
func Endpoint(hub string) (string, error) {
	base, err := url.Parse(strings.TrimRight(hub, "/") + "/")
	if err != nil || base.Host == "" {
		return "", fmt.Errorf("bad hub url %q", hub)
	}
	switch base.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return "", fmt.Errorf("bad hub url %q: want http(s)://", hub)
	}
	return base.JoinPath(strings.TrimPrefix(Path, "/")).String(), nil
}
