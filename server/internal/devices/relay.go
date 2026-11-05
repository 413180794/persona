package devices

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/devicewire"
)

// The relay (devicewire.RelayPath): connections the hub holds for the manager — a Persona Link and the streams it
// opens, the center's stream to one of this hub's computers — and the node streams the manager opens through
// another hub of the mesh, each a WebSocket the manager dials with its bus token. The hub copies messages both ways
// as they are.

// Relay is one relay connection.
type Relay = *websocket.Conn

// Accept is devices.accept: the hub holds a connection for the manager; it is taken by dialing the relay with its id.
func (d *Devices) Accept(ctx context.Context, a devicewire.Accept) error {
	if d.opts.DialRelay == nil {
		return errors.New("no relay")
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := d.opts.DialRelay(dctx, url.Values{"id": {a.ID}}.Encode())
	cancel()
	if err != nil {
		return err
	}
	switch a.Kind {
	case devicewire.AcceptLink:
		go d.acceptLink(conn, a)
	case devicewire.AcceptLinkStream:
		go d.acceptLinkStream(conn, a)
	case devicewire.AcceptNode:
		go d.serveNodeForCenter(conn, a.MachineID)
	default:
		_ = conn.Close(websocket.StatusPolicyViolation, "unknown kind "+a.Kind)
	}
	return nil
}

// openThroughHub opens a stream to the node of machine mid in hub's hub.json, through that hub of the mesh (the hub
// this manager is on opens it and relays it).
func (d *Devices) openThroughHub(ctx context.Context, hub, mid string) (*nodePipe, error) {
	if d.opts.DialRelay == nil {
		return nil, errors.New("no relay")
	}
	conn, err := d.opts.DialRelay(ctx, url.Values{"hub": {hub}, "machine": {mid}}.Encode())
	if err != nil {
		return nil, err
	}
	return wsPipe(conn), nil
}

// wsPipe is a relay carrying a byte stream as a node pipe.
func wsPipe(conn *websocket.Conn) *nodePipe {
	conn.SetReadLimit(64 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	nc := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	var once sync.Once
	return &nodePipe{r: nc, w: nc, lastErr: func() string { return "" }, close: func() {
		once.Do(func() {
			cancel()
			_ = nc.Close()
		})
	}}
}

// serveNodeForCenter opens the node of one of this hub's computers the way hub.json says, for the center that asked
// through the hub (a member drives none of its computers itself), and copies bytes until either side ends.
func (d *Devices) serveNodeForCenter(conn *websocket.Conn, mid string) {
	m := d.cfgMachine(mid)
	if m == nil {
		_ = conn.Close(websocket.StatusPolicyViolation, fmt.Sprintf("这台 hub 的 hub.json 里没有 %q", mid))
		return
	}
	octx, ocancel := context.WithCancel(context.Background())
	defer ocancel()
	pipe, err := m.openForCenter(octx)
	if err != nil { // the hub answers the center with it (a close reason is at most 123 bytes)
		_ = conn.Close(websocket.StatusTryAgainLater, clipReason(err.Error()))
		return
	}
	// Opened: the hub answers the center's upgrade now, then copies the bytes.
	wctx, wcancel := context.WithTimeout(octx, 10*time.Second)
	err = conn.Write(wctx, websocket.MessageText, []byte("open"))
	wcancel()
	if err != nil {
		pipe.close()
		_ = conn.Close(websocket.StatusGoingAway, "")
		return
	}
	d.logf("[%s] stream to the node opened for the center", mid)
	ws := wsPipe(conn)
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(pipe.w, ws.r); done <- struct{}{} }()
	go func() { _, _ = io.Copy(ws.w, pipe.r); done <- struct{}{} }()
	<-done
	pipe.close()
	ws.close()
	<-done
	d.logf("[%s] stream to the node for the center closed", mid)
}

// clipReason cuts a WebSocket close reason to what fits (123 bytes), on a character boundary.
func clipReason(s string) string {
	if len(s) <= 123 {
		return s
	}
	cut := 123
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// openForCenter opens the node the way this hub's hub.json says, each transport in turn.
func (m *Machine) openForCenter(ctx context.Context) (*nodePipe, error) {
	var failed []string
	for _, via := range m.transports() {
		p, err := m.open(ctx, via)
		if err == nil {
			return p, nil
		}
		if via == "tailcat" {
			m.tailcatReset()
		}
		failed = append(failed, via+"："+err.Error())
	}
	return nil, errors.New(strings.Join(failed, "；"))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
