package devices

import (
	"encoding/json"
	"sync"
)

// A resident node replays what it took while its manager was away (node/spool.go), each entry with a spool_id. It
// removes an entry once sent, so a repeat comes only from a node stopped between the two: the manager drops it.

// spoolSeenMost bounds the ids remembered per machine (the node's spool holds at most 500).
const spoolSeenMost = 2048

type spoolSeen struct {
	mu       sync.Mutex
	ids      map[string]bool
	order    []string
	inFlight map[string]bool // notifications being handed to the hub
}

// begin takes a replayed notification's id: false when it was handed over already (dup) or is being (busy).
func (s *spoolSeen) begin(id string) (dup, busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids[id] {
		return true, false
	}
	if s.inFlight[id] {
		return false, true
	}
	if s.inFlight == nil {
		s.inFlight = map[string]bool{}
	}
	s.inFlight[id] = true
	return false, false
}

// end is begin's notification handed over (ok: remembered) or not (tried again later).
func (s *spoolSeen) end(id string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
	if ok { // under the same lock: a try in between finds it in flight or remembered, never neither
		s.rememberLocked(id)
	}
}

// repeat says whether params carry a spool_id already seen, and remembers it when not.
func (s *spoolSeen) repeat(params json.RawMessage) bool {
	id := spoolID(params)
	if id == "" {
		return false
	}
	if s.seen(id) {
		return true
	}
	s.remember(id)
	return false
}

func spoolID(params json.RawMessage) string {
	var p struct {
		SpoolID string `json:"spool_id"`
	}
	_ = json.Unmarshal(params, &p)
	return p.SpoolID
}

func (s *spoolSeen) seen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ids[id]
}

func (s *spoolSeen) remember(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberLocked(id)
}

func (s *spoolSeen) rememberLocked(id string) {
	if s.ids[id] {
		return
	}
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	s.ids[id] = true
	s.order = append(s.order, id)
	if len(s.order) > spoolSeenMost {
		delete(s.ids, s.order[0])
		s.order = s.order[1:]
	}
}
