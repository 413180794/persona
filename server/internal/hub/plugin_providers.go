package hub

import (
	"context"
	"net/http"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
)

// The providers extension point (docs/plugins.md「模型供应商」): a plugin declares model providers — address, kind,
// models — and 设置 › 模型供应商 offers them when a launcher is added: the user picks one and fills in only the name and
// the key (POST /api/v1/launchers with preset). A provider whose plugin lists its models itself (dynamic_models) is
// asked provider.models.

// LauncherPreset is one plugin's provider as the app offers it.
type LauncherPreset struct {
	ID          string                 `json:"id"` // <plugin>/<provider>
	Plugin      string                 `json:"plugin"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Kind        string                 `json:"kind"`
	API         string                 `json:"api,omitempty"`
	BaseURL     string                 `json:"base_url"`
	Model       string                 `json:"model,omitempty"`
	SmallModel  string                 `json:"small_model,omitempty"`
	Models      []plugin.ProviderModel `json:"models,omitempty"`
	KeyHint     string                 `json:"key_hint,omitempty"`
	// ModelsError says why the plugin's own list could not be had (the declared models are still there).
	ModelsError string `json:"models_error,omitempty"`
}

// launcherPresets are the providers of every plugin switched on and able to run.
func (ps *plugins) launcherPresets(ctx context.Context) []LauncherPreset {
	out := []LauncherPreset{}
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		for _, it := range p.Manifest.Providers() {
			kind := it.Kind
			if kind == "" {
				kind = "launcher"
			}
			pr := LauncherPreset{ID: p.Record.ID + "/" + it.ID, Plugin: p.Record.ID, Name: it.Name, Description: it.Description, Kind: kind,
				API: it.API, BaseURL: it.BaseURL, Model: it.Model, SmallModel: it.SmallModel, Models: it.Models, KeyHint: it.KeyHint}
			if it.DynamicModels {
				if hp := ps.get(p.Record.ID); hp != nil && hp.proc != nil {
					cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					params, _ := plugin.Targeted(struct{}{}, it.ID)
					var res struct {
						Models []plugin.ProviderModel `json:"models"`
					}
					if err := hp.proc.Call(cctx, "provider.models", params, &res); err != nil {
						pr.ModelsError = err.Error()
					} else {
						pr.Models = mergeModels(pr.Models, res.Models)
					}
					cancel()
				}
			}
			out = append(out, pr)
		}
	}
	return out
}

// mergeModels is declared models first, then the plugin's own that are not among them.
func mergeModels(declared, listed []plugin.ProviderModel) []plugin.ProviderModel {
	out := append([]plugin.ProviderModel(nil), declared...)
	seen := map[string]bool{}
	for _, m := range declared {
		seen[m.ID] = true
	}
	for _, m := range listed {
		if m.ID != "" && !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	return out
}

// presetFill fills in what a launcher added from a preset leaves empty.
func (h *Hub) presetFill(ctx context.Context, in *launcherInput) error {
	if in.Preset == "" {
		return nil
	}
	for _, pr := range h.plugins.launcherPresets(ctx) {
		if pr.ID != in.Preset {
			continue
		}
		if in.Kind == "" {
			in.Kind = pr.Kind
		}
		if in.API == "" {
			in.API = pr.API
		}
		if in.BaseURL == "" {
			in.BaseURL = pr.BaseURL
		}
		if in.Model == "" {
			in.Model = pr.Model
			if in.Model == "" && len(pr.Models) > 0 {
				in.Model = pr.Models[0].ID
			}
		}
		if in.SmallModel == "" && in.Kind != "api" {
			in.SmallModel = pr.SmallModel
		}
		return nil
	}
	return errNoPreset
}

func (h *Hub) launcherPresetRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/launchers/presets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"presets": h.plugins.launcherPresets(r.Context())})
	})
}
