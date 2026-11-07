// Package streams carries byte streams over the hub↔node RPC connection (docs/operations/ports.md "Forwarding"): a TCP connection
// the hub forwards to a port on a node is one stream there, its bytes in stream.data events. Each direction has a
// window of bytes sent and not yet acknowledged (stream.ack), so a slow reader on one side holds its sender back
// instead of filling memory, and one big download cannot take the whole connection from everything else on it.
//
// Both ends run a Mux on their rpc.Peer: Open registers a stream (bytes for it are kept from then on), Start pumps
// it to and from a connection. The side that dials (the node) opens and starts before answering; the side that
// asked (the hub) opens before asking and starts once it has the answer, so no byte arrives for a stream nobody
// holds. A stream ends when either connection does: the end that saw it sends stream.close and the other writes
// what it already has, then closes its connection.
package streams

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Events between the two Muxes.
const (
	EventData  = "stream.data"
	EventAck   = "stream.ack"
	EventClose = "stream.close"
)

const (
	chunk = 32 << 10
	// window is how much may be in flight in one direction: enough for a page's scripts at the speed of the link,
	// small enough that a stalled reader holds little.
	window = 512 << 10
)

// Mux holds the streams of one RPC connection.
type Mux struct {
	send func(event string, params any) error

	mu      sync.Mutex
	streams map[string]*stream
	closed  bool
}

// New makes a Mux that sends its events with send (rpc.Peer.Notify).
func New(send func(event string, params any) error) *Mux {
	return &Mux{send: send, streams: map[string]*stream{}}
}

type stream struct {
	id string
	m  *Mux

	mu       sync.Mutex
	cond     *sync.Cond
	inflight int      // bytes sent and not yet acknowledged
	queue    [][]byte // bytes from the peer, not yet written to conn
	queued   int
	peerDone bool // the peer sent close: write what is queued, then close conn
	done     bool // finished: nothing more is read, written or sent
	conn     io.ReadWriteCloser
}

type dataMsg struct {
	ID   string `json:"id"`
	Data []byte `json:"data,omitempty"`
	N    int    `json:"n,omitempty"`
	Err  string `json:"error,omitempty"`
}

// Open registers stream id: bytes the peer sends for it are kept until Start.
func (m *Mux) Open(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("streams: connection closed")
	}
	if m.streams[id] != nil {
		return errors.New("streams: " + id + " is already open")
	}
	s := &stream{id: id, m: m}
	s.cond = sync.NewCond(&s.mu)
	m.streams[id] = s
	return nil
}

// Start pumps stream id to and from conn until either ends; conn is closed then. It returns at once.
func (m *Mux) Start(id string, conn io.ReadWriteCloser) {
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s == nil {
		_ = conn.Close()
		return
	}
	s.mu.Lock()
	s.conn = conn
	finished := s.done
	s.cond.Broadcast()
	s.mu.Unlock()
	if finished {
		_ = conn.Close()
		return
	}
	go s.read()
	go s.write()
}

// Abort ends stream id from this side (the dial failed, the caller went away), telling the peer.
func (m *Mux) Abort(id string, why error) {
	m.mu.Lock()
	s := m.streams[id]
	m.mu.Unlock()
	if s != nil {
		s.finish(why, true)
	}
}

// Event takes the Mux's events off the RPC connection; it reports whether event was one. It never blocks.
func (m *Mux) Event(event string, params json.RawMessage) bool {
	switch event {
	case EventData, EventAck, EventClose:
	default:
		return false
	}
	var p dataMsg
	if json.Unmarshal(params, &p) != nil {
		return true
	}
	m.mu.Lock()
	s := m.streams[p.ID]
	m.mu.Unlock()
	if s == nil {
		return true
	}
	switch event {
	case EventData:
		s.mu.Lock()
		if s.done || s.peerDone {
			s.mu.Unlock()
			return true
		}
		if s.queued+len(p.Data) > window { // the peer ignored the window: drop the stream rather than the memory
			s.mu.Unlock()
			s.finish(errors.New("streams: window exceeded"), true)
			return true
		}
		s.queue = append(s.queue, p.Data)
		s.queued += len(p.Data)
		s.cond.Broadcast()
		s.mu.Unlock()
	case EventAck:
		s.mu.Lock()
		s.inflight = max(0, s.inflight-p.N)
		s.cond.Broadcast()
		s.mu.Unlock()
	case EventClose:
		s.mu.Lock()
		s.peerDone = true
		s.cond.Broadcast()
		s.mu.Unlock()
	}
	return true
}

// CloseAll ends every stream without telling the peer (the RPC connection is gone).
func (m *Mux) CloseAll() {
	m.mu.Lock()
	m.closed = true
	all := make([]*stream, 0, len(m.streams))
	for _, s := range m.streams {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.finish(nil, false)
	}
}

// Len is how many streams are open (tests, logs).
func (m *Mux) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.streams)
}

// read sends what conn says to the peer, holding back while a window's worth is unacknowledged.
func (s *stream) read() {
	buf := make([]byte, chunk)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			s.mu.Lock()
			for s.inflight+n > window && !s.done {
				s.cond.Wait()
			}
			if s.done {
				s.mu.Unlock()
				return
			}
			s.inflight += n
			s.mu.Unlock()
			if serr := s.m.send(EventData, dataMsg{ID: s.id, Data: append([]byte(nil), buf[:n]...)}); serr != nil {
				s.finish(serr, false)
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			s.finish(err, true)
			return
		}
	}
}

// write writes what the peer sent to conn and acknowledges it; once the peer has closed and everything is written,
// the stream is finished.
func (s *stream) write() {
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.peerDone && !s.done {
			s.cond.Wait()
		}
		if s.done {
			s.mu.Unlock()
			return
		}
		if len(s.queue) == 0 { // the peer closed and all it sent is written
			s.mu.Unlock()
			s.finish(nil, false)
			return
		}
		b := s.queue[0]
		s.queue = s.queue[1:]
		s.queued -= len(b)
		conn := s.conn
		s.mu.Unlock()
		if _, err := conn.Write(b); err != nil {
			s.finish(err, true)
			return
		}
		_ = s.m.send(EventAck, dataMsg{ID: s.id, N: len(b)})
	}
}

// finish ends the stream once: conn is closed, and the peer told (tell) unless it ended it.
func (s *stream) finish(why error, tell bool) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	peerDone := s.peerDone
	conn := s.conn
	s.queue, s.queued = nil, 0
	s.cond.Broadcast()
	s.mu.Unlock()

	s.m.mu.Lock()
	delete(s.m.streams, s.id)
	s.m.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if tell && !peerDone {
		msg := dataMsg{ID: s.id}
		if why != nil {
			msg.Err = why.Error()
		}
		_ = s.m.send(EventClose, msg)
	}
}
