package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Node streams through a member (docs/mesh.md「经成员的路」): the center reaches a computer a member has in hub.json
// by asking that member, over the links, for a stream to its node. The request is an HTTP upgrade
// (Upgrade: jarvis-node) on the peer server, passed hop by hop like any other; the member opens the node the way its
// hub.json says (its local node, ssh, tailcat, the computer's jarvis-link or connector), answers 101 and from then on
// copies bytes both ways. Only the member's center may ask: a member drives none of its computers itself.

const nodeUpgrade = "jarvis-node"

// openThroughHub opens a stream to the node of machine mid in hub's hub.json, through that hub.
func (h *Hub) openThroughHub(ctx context.Context, hub, mid string) (*nodePipe, error) {
	l := h.nextLink(hub)
	if l == nil || l.transport == nil {
		return nil, fmt.Errorf("「%s」现在连不上", h.hubNameOf(hub))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://peer/api/v1/peer/node", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", nodeUpgrade)
	req.Header.Set(hdrPeerNode, hub+"/"+mid)
	req.Header.Set(hdrPeerFrom, h.hubID())
	req.Header.Set(hdrPeerTTL, itoa(meshTTL))
	client := &http.Client{Transport: l.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		msg := strings.TrimSpace(string(b))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("「%s」：%s", h.hubNameOf(hub), clip(oneLine(msg), 300))
	}
	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		resp.Body.Close()
		return nil, errors.New("the stream through the member is not writable")
	}
	var once sync.Once
	return &nodePipe{r: rwc, w: rwc, lastErr: func() string { return "" }, close: func() { once.Do(func() { _ = rwc.Close() }) }}, nil
}

// serveNodeOpen is a member opening a stream to one of its computers for its center.
func (h *Hub) serveNodeOpen(w http.ResponseWriter, r *http.Request, from, mid string) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), nodeUpgrade) {
		writeErr(w, http.StatusBadRequest, errors.New("a node stream is an upgrade"))
		return
	}
	if !h.isMember() || from == "" || from != h.centerID() {
		writeErr(w, http.StatusForbidden, errors.New("只有这台 hub 所在组的中心能经它连电脑"))
		return
	}
	m := h.cfgMachine(mid)
	if m == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("这台 hub 的 hub.json 里没有 %q", mid))
		return
	}
	// Not the request's context: an ssh process lives as long as the context it was started with, and the stream
	// outlives this handler's view of the request once the connection is taken over.
	octx, ocancel := context.WithCancel(context.Background())
	defer ocancel()
	pipe, err := m.openForCenter(octx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		pipe.close()
		writeErr(w, http.StatusInternalServerError, errors.New("cannot take the connection over"))
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		pipe.close()
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+nodeUpgrade+"\r\n\r\n"); err != nil {
		pipe.close()
		conn.Close()
		return
	}
	h.logf("[%s] stream to the node opened for the center", mid)
	joinNode(conn, buf, pipe)
	h.logf("[%s] stream to the node for the center closed", mid)
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

// joinNode copies between the center's stream and the node until either ends.
func joinNode(conn net.Conn, buf *bufio.ReadWriter, pipe *nodePipe) {
	done := make(chan struct{}, 2)
	go func() {
		var src io.Reader = conn
		if buf != nil && buf.Reader.Buffered() > 0 {
			src = io.MultiReader(io.LimitReader(buf.Reader, int64(buf.Reader.Buffered())), conn)
		}
		_, _ = io.Copy(pipe.w, src)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, pipe.r)
		done <- struct{}{}
	}()
	<-done
	pipe.close()
	conn.Close()
	<-done
}
