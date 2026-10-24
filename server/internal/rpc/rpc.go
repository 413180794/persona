// Package rpc is a tiny bidirectional JSON-lines RPC used between hub and node over SSH stdio.
//
// Each line is one of:
//
//	{"id":"h1","method":"m","params":{}}      request
//	{"id":"h1","result":{}} / {"id":"h1","error":{"message":"…"}}   response
//	{"event":"e","params":{}}                 one-way event
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
}

func (e *Error) Error() string { return e.Message }

var ErrClosed = errors.New("rpc: connection closed")

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
}

// New creates a peer. prefix distinguishes request ids by side ("h" for hub, "n" for node).
func New(prefix string, w io.Writer) *Peer {
	return &Peer{prefix: prefix, w: w, pending: map[string]chan message{}, closed: make(chan struct{})}
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
			go p.handle(ctx, m)
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
	var res any
	var err error
	if p.OnRequest == nil {
		err = fmt.Errorf("no handler for %s", m.Method)
	} else {
		res, err = p.OnRequest(ctx, m.Method, m.Params)
	}
	out := message{ID: m.ID}
	if err != nil {
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
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	p.wmu.Lock()
	defer p.wmu.Unlock()
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
	if err := p.write(message{ID: id, Method: method, Params: raw}); err != nil {
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
		return ctx.Err()
	case <-p.closed:
		return ErrClosed
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
