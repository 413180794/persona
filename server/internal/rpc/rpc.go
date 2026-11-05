// Package rpc is a tiny bidirectional JSON-lines RPC used between hub and node over SSH stdio.
//
// Each line is one of:
//
//	{"id":"h1","method":"m","params":{}}      request
//	{"id":"h1","result":{}} / {"id":"h1","error":{"message":"…"}}   response
//	{"event":"e","params":{}}                 one-way event
//	{"event":"rpc.cancel","params":{"id":"h1"}}   the caller no longer waits for request h1 (Peer.Cancels)
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

type EventHandler func(event string, params json.RawMessage)

type message struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Event  string          `json:"event,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type Error struct {
	Message string `json:"message"`
	// Code and Data are set by handlers that return an *Error themselves, for callers that act on the kind of
	// failure (a refused send) rather than show the message.
	Code string          `json:"code,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// WireError is its code and data, for the extension SDK (sdk/go/ext) to send as they are.
func (e *Error) WireError() (string, json.RawMessage) { return e.Code, e.Data }

var ErrClosed = errors.New("rpc: connection closed")

// ErrDropped is what Call answers when the connection closed after the request went out: the other side may have done
// it. It is ErrClosed too (errors.Is), with the same words (a relay further on passes them as they are).
var ErrDropped error = droppedError{}

type droppedError struct{}

func (droppedError) Error() string        { return ErrClosed.Error() }
func (droppedError) Is(target error) bool { return target == ErrClosed }

type Peer struct {
	prefix  string
	w       io.Writer
	wmu     sync.Mutex
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[string]chan message
	closed  chan struct{}
	once    sync.Once
	err     error

	OnRequest Handler
	OnEvent   EventHandler
	// Cancels: the methods whose calls tell the other side when their ctx ends first (cancelEvent), which cancels
	// that request's ctx. None by default: extensions and connectors speak this protocol too and need not know the
	// event, and most calls are meant to finish on the other side anyway. A peer that does not know the event ignores
	// it, as before: its handler runs on.
	Cancels map[string]bool

	running map[string]context.CancelFunc // requests being handled, by id: what a cancelEvent ends
}

// cancelEvent is sent for a request the caller no longer waits for: an approval the hub asks for on the phone stays
// pending otherwise, and allowing it later runs a tool call nobody is waiting on (a stale message sent, #2597).
const cancelEvent = "rpc.cancel"

// New creates a peer. prefix distinguishes request ids by side ("h" for hub, "n" for node).
func New(prefix string, w io.Writer) *Peer {
	return &Peer{prefix: prefix, w: w, pending: map[string]chan message{}, running: map[string]context.CancelFunc{}, closed: make(chan struct{})}
}

// Serve reads messages until r fails; it returns the read error.
func (p *Peer) Serve(ctx context.Context, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		switch {
		case m.Method != "":
			// Registered before the handler starts: a cancel right behind the request finds it.
			hctx, cancel := context.WithCancel(ctx)
			p.mu.Lock()
			p.running[m.ID] = cancel
			p.mu.Unlock()
			go p.handle(hctx, m)
		case m.Event == cancelEvent:
			var c struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(m.Params, &c)
			p.mu.Lock()
			cancel := p.running[c.ID]
			p.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case m.Event != "":
			if p.OnEvent != nil {
				p.OnEvent(m.Event, m.Params)
			}
		case m.ID != "":
			p.mu.Lock()
			ch := p.pending[m.ID]
			delete(p.pending, m.ID)
			p.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	p.Close(err)
	return err
}

func (p *Peer) handle(ctx context.Context, m message) {
	defer func() {
		p.mu.Lock()
		cancel := p.running[m.ID]
		delete(p.running, m.ID)
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}()
	var res any
	var err error
	if p.OnRequest == nil {
		err = fmt.Errorf("no handler for %s", m.Method)
	} else {
		res, err = p.OnRequest(ctx, m.Method, m.Params)
	}
	out := message{ID: m.ID}
	var coded *Error
	if errors.As(err, &coded) {
		out.Error = coded
	} else if err != nil {
		out.Error = &Error{Message: err.Error()}
	} else {
		b, merr := json.Marshal(res)
		if merr != nil {
			out.Error = &Error{Message: merr.Error()}
		} else {
			out.Result = b
		}
	}
	_ = p.write(out)
}

func (p *Peer) write(m message) error {
	return p.writeContext(context.Background(), m)
}

func (p *Peer) writeContext(ctx context.Context, m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	p.wmu.Lock()
	defer p.wmu.Unlock()
	// Serialization or another write may have outlasted the caller. Check at dispatch.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-p.closed:
		return ErrClosed
	default:
	}
	_, err = p.w.Write(b)
	return err
}

// Call sends a request and decodes the result into out (if non-nil).
func (p *Peer) Call(ctx context.Context, method string, params any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := fmt.Sprintf("%s%d", p.prefix, p.nextID.Add(1))
	ch := make(chan message, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := p.writeContext(ctx, message{ID: id, Method: method, Params: raw}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		if p.Cancels[method] {
			params, _ := json.Marshal(map[string]string{"id": id})
			_ = p.write(message{Event: cancelEvent, Params: params})
		}
		return ctx.Err()
	case <-p.closed:
		return ErrDropped
	}
}

// Notify sends a one-way event.
func (p *Peer) Notify(event string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.write(message{Event: event, Params: raw})
}

func (p *Peer) Close(err error) {
	p.once.Do(func() {
		p.err = err
		close(p.closed)
	})
}

func (p *Peer) Done() <-chan struct{} { return p.closed }

// Err is why the peer was closed (what Close was given); nil while it is open.
func (p *Peer) Err() error {
	select {
	case <-p.closed:
		return p.err
	default:
		return nil
	}
}
