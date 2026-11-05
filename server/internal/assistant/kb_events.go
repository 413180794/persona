package assistant

import (
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
)

// kbEventPublisher is optional for in-process embedders. Production's BusHub publishes only these two typed KB
// events; they never enter the public ws.broadcast path.
type kbEventPublisher interface {
	KBEvent(event string, params any) error
}

func (b *BusHub) KBEvent(event string, params any) error { return b.notifyHub(event, params) }

type kbNotice struct {
	event   string
	changed assistantwire.KBChanged
	source  string
}

type kbEventQueue struct {
	mu      sync.Mutex
	pending []kbNotice
	running bool
}

const kbEventLimit = 512

func (s *Service) kbChanged(change kb.Change) {
	s.kbQueueEvent(kbNotice{event: assistantwire.EventKBChanged,
		changed: assistantwire.KBChanged{Kind: change.Kind, ID: change.ID, What: change.What}})
}

func (s *Service) kbForgotten(source string) {
	s.kbQueueEvent(kbNotice{event: assistantwire.EventKBForgotten, source: source})
}

// kbQueueEvent keeps writes independent of a slow or disconnected bus. Identical pending invalidations coalesce;
// overload drops an older hint. These notifications are best effort, like the existing extension events.
func (s *Service) kbQueueEvent(n kbNotice) {
	if _, ok := s.hub.(kbEventPublisher); !ok {
		return
	}
	q := &s.kb.events
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, old := range q.pending {
		if old == n {
			return
		}
	}
	if len(q.pending) >= kbEventLimit {
		q.pending = q.pending[1:]
	}
	q.pending = append(q.pending, n)
	if !q.running {
		q.running = true
		go s.kbSendEvents()
	}
}

func (s *Service) kbSendEvents() {
	publisher := s.hub.(kbEventPublisher)
	q := &s.kb.events
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		n := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()
		var params any = n.changed
		if n.event == assistantwire.EventKBForgotten {
			params = assistantwire.KBForgottenEvent{Source: n.source}
		}
		if publisher.KBEvent(n.event, params) != nil {
			s.logf("kb: change notification could not be delivered")
		}
	}
}
