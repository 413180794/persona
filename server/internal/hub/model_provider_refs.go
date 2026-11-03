package hub

import "context"

// providerModelUses prevents a managed credential/profile from disappearing underneath an explicit binding.
func (h *Hub) providerModelUses(ctx context.Context, provider string) ([]string, error) {
	uses := []string{}
	for _, id := range modelCategoryIDs {
		p, err := h.modelPurposeDef(ctx, id)
		if err != nil {
			return nil, err
		}
		b, err := h.resolveModel(ctx, id)
		if err != nil {
			return nil, err
		}
		if b.Provider == provider {
			uses = append(uses, p.Name)
		}
	}
	return uses, nil
}
