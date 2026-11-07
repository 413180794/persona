package hub

import (
	"context"
	"encoding/json"
	"time"
)

// Where a sent message landed in the transcript (docs/protocol/protocol.md "Matching pending messages"). The node ties a send with a
// client_msg_id to the transcript line that is its copy and puts the id on that line's items (node/echo.go); it tells
// the hub what it expects and what it found (session.echo), which the hub keeps with the send (sent_messages.echo),
// and hands back to a node that started again (session.echoes, on every connect): the items keep their id, and a
// message still expected is found after the restart too.

// echoStore is the part of the store that keeps them (db.DB); a store without it keeps nothing.
type echoStore interface {
	RecordEcho(ctx context.Context, id, machine string, echo json.RawMessage) error
	Echoes(ctx context.Context, machine string) ([]json.RawMessage, error)
}

func (h *Hub) echoes() echoStore {
	if h.store == nil {
		return nil
	}
	s, _ := h.store.(echoStore)
	return s
}

// recordEcho keeps a node's session.echo, off the event loop.
func (m *Machine) recordEcho(params json.RawMessage) {
	s := m.hub.echoes()
	var p struct {
		ClientMsgID string `json:"client_msg_id"`
	}
	if s == nil || json.Unmarshal(params, &p) != nil || p.ClientMsgID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.RecordEcho(ctx, p.ClientMsgID, m.cfg.ID, params); err != nil {
			m.hub.logf("[%s] echo %s: %v", m.cfg.ID, p.ClientMsgID, err)
		}
	}()
}

// echoesToSend is what was kept of a node's sends of the last day, handed to it when it connects (session.echoes,
// devices.connected; an older node does not know the method).
func (m *Machine) echoesToSend(ctx context.Context) []json.RawMessage {
	s := m.hub.echoes()
	if s == nil {
		return nil
	}
	list, err := s.Echoes(ctx, m.cfg.ID)
	if err != nil {
		m.hub.logf("[%s] echoes: %v", m.cfg.ID, err)
		return nil
	}
	return list
}
