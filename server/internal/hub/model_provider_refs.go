package hub

import "context"

// providerModelUses prevents a managed credential/profile from disappearing underneath an explicit binding.
func (h *Hub) providerModelUses(ctx context.Context, provider string) ([]string, error) {
	purposes := append([]ModelPurpose(nil), modelPurposes...)
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	for _, t := range s.tasks {
		purposes = append(purposes, ModelPurpose{ID: "task." + t.ID, Name: t.label()})
	}
	s.mu.Unlock()
	uses := []string{}
	for _, p := range purposes {
		b, err := h.resolveModel(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if b.Provider == provider {
			uses = append(uses, p.Name)
		}
	}
	return uses, nil
}
