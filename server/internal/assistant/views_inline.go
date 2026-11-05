package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

type inlineViewStore interface {
	RegisterAssistantViewBlock(context.Context, db.AssistantViewBlockRow) (bool, error)
	AssistantViewBlockKeys(context.Context, string) ([]string, error)
	RecordAssistantViewErrors(context.Context, string, string, string, []string, time.Time) error
	TakeAssistantViewErrors(context.Context, string, string) ([]string, error)
}

func (s *Service) registerInlineViews(parent context.Context, p SessionItems) {
	if !s.hello(parent).PersonaView {
		return
	}
	st, ok := s.store().(inlineViewStore)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if session, known := s.session(ctx, p.MachineID, p.SessionID); !known || !session.Assistant {
		return
	}
	for _, it := range p.Items {
		if it.ID == "" || it.Role != "assistant" || it.Kind != proto.KindText {
			continue
		}
		if it.FullSize > 0 {
			var full proto.Item
			if err := s.call(ctx, p.MachineID, "session.item", map[string]any{"session_id": p.SessionID, "item_id": it.ID}, &full); err != nil || full.ID != it.ID || full.Role != "assistant" || full.Kind != proto.KindText || full.FullSize > 0 {
				continue
			}
			it = full
		}
		if !wire.HasAssistantView(it.Text) {
			continue
		}
		at := it.TS
		if at.IsZero() {
			at = time.Now().UTC()
		}
		var problems []string
		for _, b := range wire.AssistantViewBlocks(it.Text) {
			if b.Language != "persona-view" {
				continue
			}
			v, err := b.Parse()
			if err != nil {
				problems = append(problems, fmt.Sprintf("persona-view block %d: %v", b.Index, err))
				continue
			}
			raw, err := json.Marshal(v)
			if err != nil {
				continue
			}
			row := db.AssistantViewBlockRow{Conversation: p.SessionID, Key: wire.AssistantBlockKey(it.ID, b.Index), MachineID: p.MachineID, ItemID: it.ID, Index: b.Index, View: raw, CreatedAt: at}
			if created, err := st.RegisterAssistantViewBlock(ctx, row); err != nil {
				s.logf("assistant inline view registration failed: %v", err)
			} else if created {
				s.event(ctx, map[string]any{"type": "assistant_view_block", "conversation": p.SessionID, "key": row.Key})
			}
		}
		if err := st.RecordAssistantViewErrors(ctx, p.SessionID, p.MachineID, it.ID, problems, at); err != nil {
			s.logf("assistant view errors could not be recorded: %v", err)
		}
	}
}

// registerInlineTurn also registers cards when no app was watching the conversation during its reply.
func (s *Service) registerInlineTurn(parent context.Context, r SessionRef) {
	if !s.hello(parent).PersonaView {
		return
	}
	if _, ok := s.store().(inlineViewStore); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	if session, known := s.session(ctx, r.MachineID, r.SessionID); !known || !session.Assistant {
		return
	}
	var result struct {
		Items []proto.Item `json:"items"`
	}
	if err := s.call(ctx, r.MachineID, DeviceSessionItems, map[string]any{"session_id": r.SessionID, "turns": 1, "lazy_images": true}, &result); err != nil {
		return
	}
	s.registerInlineViews(ctx, SessionItems{MachineID: r.MachineID, SessionID: r.SessionID, Items: result.Items})
}

func (s *Service) conversationViewBlockKeys(ctx context.Context, conversation string) ([]string, error) {
	st, ok := s.store().(inlineViewStore)
	if !ok {
		return []string{}, nil
	}
	return st.AssistantViewBlockKeys(ctx, conversation)
}
