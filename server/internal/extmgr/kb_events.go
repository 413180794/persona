package extmgr

import (
	"encoding/json"
	"slices"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
)

func init() {
	onAssistantEvent(assistantwire.EventKBChanged, func(h *Manager, raw json.RawMessage) {
		var p assistantwire.KBChanged
		if json.Unmarshal(raw, &p) != nil || p.ID == "" || !slices.Contains([]string{kb.KindChat, kb.KindPerson, kb.KindProject, kb.KindDoc, kb.KindMeeting, kb.KindEvent, kb.KindRecording}, p.Kind) ||
			!slices.Contains([]string{"metadata", "summary", "messages", "log"}, p.What) {
			return
		}
		h.extensions.queueKBEvent(assistantwire.EventKBChanged, map[string]any{"kind": p.Kind, "id": p.ID, "what": p.What})
	})
	onAssistantEvent(assistantwire.EventKBForgotten, func(h *Manager, raw json.RawMessage) {
		var p assistantwire.KBForgottenEvent
		if json.Unmarshal(raw, &p) != nil || p.Source == "" {
			return
		}
		h.extensions.queueKBEvent(assistantwire.EventKBForgotten, map[string]any{"source": p.Source})
	})
}

type kbExtensionNotice struct {
	topic   string
	payload map[string]any
}
type kbExtensionEvents struct {
	mu      sync.Mutex
	running bool
	pending []kbExtensionNotice
}

// queueKBEvent copies only the allowlisted fields parsed above. At most one worker sends these notifications;
// overload drops the oldest hint, so a slow extension cannot hold up the assistant's bus or grow an unbounded queue.
func (ps *extensions) queueKBEvent(topic string, payload map[string]any) {
	if ps == nil {
		return
	}
	q := &ps.kbEvents
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) >= 512 {
		q.pending = q.pending[1:]
	}
	q.pending = append(q.pending, kbExtensionNotice{topic, payload})
	if !q.running {
		q.running = true
		go ps.sendKBEvents()
	}
}

func (ps *extensions) sendKBEvents() {
	q := &ps.kbEvents
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
		ps.emit(n.topic, n.payload)
	}
}
