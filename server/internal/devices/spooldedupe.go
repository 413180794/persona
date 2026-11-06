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
	mu    sync.Mutex
	ids   map[string]bool
	order []string
}

// repeat says whether params carry a spool_id already seen, and remembers it when not.
func (s *spoolSeen) repeat(params json.RawMessage) bool {
	var p struct {
		SpoolID string `json:"spool_id"`
	}
	if json.Unmarshal(params, &p) != nil || p.SpoolID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids[p.SpoolID] {
		return true
	}
	if s.ids == nil {
		s.ids = map[string]bool{}
	}
	s.ids[p.SpoolID] = true
	s.order = append(s.order, p.SpoolID)
	if len(s.order) > spoolSeenMost {
		delete(s.ids, s.order[0])
		s.order = s.order[1:]
	}
	return false
}
