package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/db"
)

const feedView = "view"

// viewStore is kept separate so older test stores fail closed instead of creating non-durable cards.
type viewStore interface {
	CreateAssistantView(context.Context, db.AssistantViewRow, db.FeedRow) error
	AssistantView(context.Context, string, string) (*db.AssistantViewRow, error)
	AssistantViews(context.Context, string) ([]db.AssistantViewRow, error)
	UpdateAssistantView(context.Context, string, string, int64, json.RawMessage, time.Time) (*db.AssistantViewRow, error)
}

type viewNotifications struct {
	mu      sync.Mutex
	pending map[string]wire.AssistantView
}

func (s *Service) viewsStore() (viewStore, error) {
	st, ok := s.store().(viewStore)
	if !ok {
		return nil, errors.New("live views require a durable database")
	}
	return st, nil
}

func storedView(row db.AssistantViewRow) (wire.AssistantView, error) {
	var v wire.View
	if err := json.Unmarshal(row.View, &v); err != nil {
		return wire.AssistantView{}, err
	}
	return wire.AssistantView{ID: row.ID, Conversation: row.Conversation, Version: row.Version, View: v, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func (s *Service) toolView(ctx context.Context, machine, conversation, name string, raw json.RawMessage) (string, error) {
	if !s.hello(ctx).PersonaView {
		return "", errors.New("assistant views are disabled")
	}
	if machine == "" || conversation == "" {
		return "", errors.New("forbidden: view tools require the calling assistant conversation")
	}
	if session, ok := s.session(ctx, machine, conversation); !ok || !session.Assistant {
		return "", errors.New("forbidden: only assistant conversations may create or update views")
	}
	st, err := s.viewsStore()
	if err != nil {
		return "", err
	}
	var in struct {
		ID      string          `json:"id"`
		View    json.RawMessage `json:"view"`
		Version int64           `json:"version"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", err
	}
	if name == "view_show" {
		v, err := wire.ParseAssistantView(in.View)
		if err != nil {
			return "", fmt.Errorf("invalid view: %w", err)
		}
		view, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		at := time.Now().UTC()
		row := db.AssistantViewRow{ID: "view_" + randomHex(8), Conversation: conversation, MachineID: machine, View: view, Version: 1, CreatedAt: at, UpdatedAt: at}
		out, err := storedView(row)
		if err != nil {
			return "", err
		}
		body, err := json.Marshal(map[string]any{"view": out})
		if err != nil {
			return "", err
		}
		feed := db.FeedRow{ID: "feed_" + randomHex(8), Conversation: conversation, At: at, Kind: feedView, Ref: row.ID, Body: body}
		if err := st.CreateAssistantView(ctx, row, feed); err != nil {
			return "", err
		}
		s.event(ctx, map[string]any{"type": "assistant_feed", "conversation": conversation, "item": FeedItem{ID: feed.ID, Conversation: conversation, At: at, Kind: feedView, Ref: row.ID, Body: body}})
		s.viewChanged(out)
		return viewToolJSON(map[string]any{"id": row.ID, "version": row.Version})
	}
	if in.ID == "" {
		return "", errors.New("id is required")
	}
	row, err := st.AssistantView(ctx, conversation, in.ID)
	if errors.Is(err, db.ErrAssistantViewNotFound) {
		return "", errors.New("forbidden: no view with this id in the calling conversation")
	}
	if err != nil {
		return "", err
	}
	if row.MachineID != machine {
		return "", errors.New("forbidden: this card was created by an assistant conversation on another machine")
	}
	if name == "view_get" {
		out, err := storedView(*row)
		if err != nil {
			return "", err
		}
		actions := []wire.ViewAction{}
		if actionStore, ok := st.(viewActionStore); ok {
			a, err := actionStore.AssistantViewAction(ctx, conversation, in.ID)
			if err != nil {
				return "", err
			}
			if a != nil {
				actions = append(actions, wireViewAction(*a))
			}
		}
		return viewToolJSON(struct {
			wire.AssistantView
			Actions []wire.ViewAction `json:"actions"`
		}{AssistantView: out, Actions: actions})
	}
	v, err := wire.ParseAssistantView(in.View)
	if err != nil {
		return "", fmt.Errorf("invalid view: %w", err)
	}
	view, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	version := in.Version
	if version == 0 {
		version = row.Version
	}
	updated, err := st.UpdateAssistantView(ctx, conversation, in.ID, version, view, time.Now().UTC())
	if errors.Is(err, db.ErrAssistantViewVersion) {
		return "", errors.New("version conflict: call view_get, then update from its version")
	}
	if err != nil {
		return "", err
	}
	out, err := storedView(*updated)
	if err != nil {
		return "", err
	}
	s.viewChanged(out)
	return viewToolJSON(map[string]any{"version": out.Version})
}

func viewToolJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	return string(raw), err
}

// Persist first; only notifications are coalesced. A restart loses no card contents or feed positions.
func (s *Service) viewChanged(v wire.AssistantView) {
	b := &s.viewEvents
	b.mu.Lock()
	if b.pending == nil {
		b.pending = map[string]wire.AssistantView{}
	}
	old, exists := b.pending[v.ID]
	if !exists || old.Version < v.Version {
		b.pending[v.ID] = v
	}
	if exists {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	time.AfterFunc(time.Second, func() {
		b.mu.Lock()
		latest := b.pending[v.ID]
		delete(b.pending, v.ID)
		b.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.event(ctx, map[string]any{"type": "assistant_view", "conversation": latest.Conversation, "view": latest})
	})
}

func (s *Service) conversationViews(ctx context.Context, conversation string) (map[string]wire.AssistantView, error) {
	out := map[string]wire.AssistantView{}
	st, err := s.viewsStore()
	if err != nil {
		return out, nil
	}
	rows, err := st.AssistantViews(ctx, conversation)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		v, err := storedView(row)
		if err != nil {
			return nil, err
		}
		out[v.ID] = v
	}
	return out, nil
}
