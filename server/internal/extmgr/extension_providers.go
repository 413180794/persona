package extmgr

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
)

// The providers extension point (docs/extensions/extensions.md "Model providers"): a plugin declares model providers — address,
// kind, models — and Settings › Model providers offers them when a launcher is added: the user picks one and fills in
// only the name and the key (POST /api/v1/launchers with preset). A provider whose plugin lists its models itself
// (dynamic_models) is asked provider.models.

// LauncherPreset is one extension's provider as the app offers it (extwire).
type LauncherPreset = assistantwire.LauncherPreset

// launcherPresets are the providers of every plugin switched on and able to run.
func (ps *extensions) launcherPresets(ctx context.Context) []LauncherPreset {
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
			pr := LauncherPreset{ID: p.Record.ID + "/" + it.ID, Extension: p.Record.ID, Name: it.Name, Description: it.Description, Kind: kind,
				API: it.API, BaseURL: it.BaseURL, Model: it.Model, SmallModel: it.SmallModel, Models: it.Models, KeyHint: it.KeyHint}
			if it.DynamicModels {
				if hp := ps.get(p.Record.ID); hp != nil && hp.proc != nil {
					cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					params, _ := extension.Targeted(struct{}{}, it.ID)
					var res struct {
						Models []extension.ProviderModel `json:"models"`
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
func mergeModels(declared, listed []extension.ProviderModel) []extension.ProviderModel {
	out := append([]extension.ProviderModel(nil), declared...)
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
