package assistant

import (
	"context"

	"github.com/nwverse/persona/server/internal/kb"
)

// kbModel is the knowledge base's model for one purpose (kb.compress, kb.project, kb.items, kb.dedupe, kb.moments):
// the hub's model configuration, asked over the bus (app.call models.json with the room for the answer) the way the
// assistant's other model calls are, until it has models of its own. A paused model comes back as one (ai.IsPaused).
func (s *Service) kbModel(purpose string) kb.Model { return kbModel{s: s, purpose: purpose} }

type kbModel struct {
	s       *Service
	purpose string
}

func (m kbModel) JSONMax(ctx context.Context, system, user string, maxTokens int, out any) error {
	return m.s.appCall(ctx, AppModels, ModelsJSON, map[string]any{"purpose": m.purpose, "system": system, "input": user,
		"max_tokens": maxTokens}, out)
}
