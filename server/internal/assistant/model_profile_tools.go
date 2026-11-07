package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Profile tools expose the same operations as settings, without copying credentials into model context.
func (s *Service) toolModelProfile(ctx context.Context, self, name string, args json.RawMessage) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	by := ProfileActor{Kind: "assistant"}
	switch name {
	case "model_profile_switch":
		if a.ID == "" {
			return "", fmt.Errorf("id is required; use model_profiles_list")
		}
		if _, err := s.applyModelProfile(ctx, a.ID, by); err != nil {
			return "", err
		}
	case "model_profile_rollback":
		if _, err := s.rollbackModelProfile(ctx, by); err != nil {
			return "", err
		}
	}
	v, err := s.modelProfilesView(ctx)
	if err != nil {
		return "", err
	}
	var result any = v
	if name == "model_profile_get" {
		i := slices.IndexFunc(v.Profiles, func(p ModelProfileView) bool { return p.ID == a.ID })
		if i < 0 {
			return "", errModelProfileNotFound
		}
		result = v.Profiles[i]
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}

func (s *Service) applyStartTier(ctx context.Context, params map[string]any) error {
	tier, _ := params["tier"].(string)
	delete(params, "tier") // node runtimes need only the resolved fields
	if tier == "" {
		return nil
	}
	t, err := s.tierModel(ctx, tier)
	if err != nil {
		return err
	}
	launcher, _ := params["launcher"].(string)
	model, _ := params["model"].(string)
	effort, _ := params["effort"].(string)
	// An explicit different model does not inherit effort intended for the tier's model.
	if model != "" && model != t.Model {
		t.Effort = ""
	}
	withTier(t, &launcher, &model, &effort)
	params["launcher"], params["model"], params["effort"] = launcher, model, effort
	return nil
}

func (s *Service) applyTaskTier(ctx context.Context, t *PlanTask) error {
	p := map[string]any{"tier": t.Tier, "launcher": t.Launcher, "model": t.Model, "effort": t.Effort}
	if err := s.applyStartTier(ctx, p); err != nil {
		return err
	}
	t.Launcher, _ = p["launcher"].(string)
	t.Model, _ = p["model"].(string)
	t.Effort, _ = p["effort"].(string)
	return nil
}

// Read this outside the ordinary source cache so every turn observes a switch, including a rollback to no profile.
func (s *Service) modelProfileContext(ctx context.Context) string {
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	ps, _, err := s.readModelProfiles(ctx)
	if err != nil {
		return "unavailable (model_profiles_list to retry)"
	}
	st, err := s.readModelProfileState(ctx)
	if err != nil {
		return "unavailable (model_profiles_list to retry)"
	}
	if st.Active == "" {
		return ""
	}
	i := slices.IndexFunc(ps.Profiles, func(p ModelProfile) bool { return p.ID == st.Active })
	if i < 0 {
		return st.Active
	}
	p := ps.Profiles[i]
	parts := []string{p.Name + " (" + p.ID + ")"}
	for _, name := range slices.Sorted(maps.Keys(p.Tiers)) {
		t := p.Tiers[name]
		parts = append(parts, name+"="+t.Launcher+"/"+t.Model)
	}
	return strings.Join(parts, "; ")
}
